// Package stats implements the stats query API.
//
//	GET /api/v1/admin/env/{env}/stats?since=7d
//
// A small read-model over the events rows that T9 persists. Aggregate
// counts only — userHash never leaves the server through this endpoint
// (no raw-event export, no PII in the response).
//
// SINCE GRAMMAR: ?since=<N>d (days), default 7d when absent, clamped to a
// max of 90d. Malformed values (abc, -5d, 0d, 7h, bare numbers) -> 400.
// FLAG FILTER: ?flag=<key> scopes counts to one flag (max flagkey.MaxLen
// chars, longer -> 400); absent/empty stays env-wide. The verbatim key is
// resolved once per request to the stored flag record ID scoped to the
// env's project (key + project, never global key) and the ID — never
// the verbatim key — is what rows/buckets are matched against, so
// renamed/orphaned flags keep their history. Unknown
// flags answer zeros with flagFound:false. Auth is superuser-only via
// apis.RequireSuperuserAuth().
//
// SCALE NOTE: aggregation runs in-Go over indexed filter queries —
// loadRows selects events by (env, ts >= cutoff) and loadRollups
// selects event_daily by (env, cutoffDay <= day < horizon), served
// by idx_events_env_ts and idx_event_daily_upsert_key /
// idx_event_daily_day. The frozen Aggregate / AggregateRollups helpers
// re-apply the same predicates in-Go, so the DB prefilter is a pure
// subset narrowing: identical results, no full scan.
package stats

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/flagkey"
	"github.com/configwire/configwire/purge"
	"github.com/configwire/configwire/releases"
	"github.com/configwire/configwire/retention"
	"github.com/configwire/configwire/security"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// DefaultSinceDays applies when ?since= is absent.
const (
	DefaultSinceDays = 7
	// MaxSinceDays clamps large ?since= values (e.g. 400d -> 90d).
	MaxSinceDays = 90
	// Flag keys are bounded by flagkey.MaxLen, shared with publish and
	// ingest so a published flag is always stats-queryable.
)

// maxSinceDays returns the active ?since= clamp from the retention
// singleton (rollupDays): stats history cannot reach past the
// daily-aggregate window. Defaults to MaxSinceDays (90) matching the
// historic rollup window.
func maxSinceDays() int {
	if n := retention.GetConfig().RollupDays; n >= 1 {
		return n
	}
	return MaxSinceDays
}

// ParseSince parses the ?since= query value purely (no I/O), so it is
// unit-testable. "" -> DefaultSinceDays. "<N>d" with N >= 1 -> N clamped
// to the configured rollup window. Anything else (abc, -5d, 0d, 7h, "7")
// -> error, which the handler maps to 400.
func ParseSince(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultSinceDays, nil
	}
	s := strings.TrimSpace(raw)
	if !strings.HasSuffix(s, "d") {
		return 0, errBadSince()
	}
	n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
	if err != nil || n < 1 {
		return 0, errBadSince()
	}
	if max := maxSinceDays(); n > max {
		n = max
	}
	return n, nil
}

type sinceError struct{ msg string }

func (e *sinceError) Error() string { return e.msg }

func errBadSince() error {
	return &sinceError{msg: fmt.Sprintf("invalid since: must be <N>d (1-%d days), e.g. 7d.", maxSinceDays())}
}

// ParseFlag validates the ?flag= query value purely (no I/O), so it is
// unit-testable. "" -> "" (no filter, env-wide). Otherwise the verbatim
// string (no trimming/normalization); longer than flagkey.MaxLen -> error,
// which the handler maps to 400.
func ParseFlag(raw string) (string, error) {
	if len(raw) > flagkey.MaxLen {
		return "", &sinceError{msg: fmt.Sprintf("invalid flag: must be at most %d characters.", flagkey.MaxLen)}
	}
	return raw, nil
}

// flagOnly normalizes the optional variadic flagFilter: absent or empty
// means env-wide (""); otherwise the verbatim flag key to scope to.
func flagOnly(flagFilter []string) string {
	if len(flagFilter) == 0 {
		return ""
	}
	return flagFilter[0]
}

// EventRow is the minimal event projection Aggregate needs — pure and
// DB-free so aggregation is unit-testable on fake rows. Without a flag
// filter stats aggregate env-wide; with one only matching flags count.
type EventRow struct {
	EnvID   string
	Flag    string // verbatim flag key ("" when the column is absent/unset — GetString reads both "" and NULL as "")
	Kind    string // "fetch" | "exposure"
	Variant string // verbatim stored variant ("" possible, counted as-is)
	Version int
	Ts      time.Time
}

// Stats is the aggregation result: exact integer counts, no rounding.
type Stats struct {
	Fetches    int
	Exposures  int
	PerVariant map[string]int // over exposures only, verbatim variant names
	PerVersion map[int]int    // over fetches only, by fetched release version
}

// RatesFor derives per-variant exposure shares purely (display-math
// only; counts are never rounded). exposures == 0 yields an empty
// non-nil map so the wire shape is rates:{} (never NaN/null).
func RatesFor(st Stats) map[string]float64 {
	rates := map[string]float64{}
	if st.Exposures <= 0 {
		return rates
	}
	for v, n := range st.PerVariant {
		rates[v] = float64(n) / float64(st.Exposures)
	}
	return rates
}

// TotalFor sums fetches and exposures into the wire total.
func TotalFor(st Stats) int {
	return st.Fetches + st.Exposures
}

// EffectiveSince renders the effective window label after ParseSince
// clamping (e.g. 400d -> "90d", absent -> "7d").
func EffectiveSince(days int) string {
	return strconv.Itoa(days) + "d"
}

// EchoFor builds the echo block purely (unit-testable): env is the
// verbatim request value, since/sinceDays/horizon carry the EFFECTIVE
// window after the configured rollup-window clamp, cutoff is the UTC RFC3339 window start,
// and rollupHorizon is the UTC RFC3339 midnight that splits raw events
// from pre-purge daily rollups (see HorizonFor). horizon keeps its original
// label meaning ("7d"); rollupHorizon is the additive machine-readable
// split point, so old clients keep parsing horizon untouched.
// The optional flag (pass only for ?flag= requests) echoes back as
// echo.flag; without it the key stays absent (no per-key entry).
func EchoFor(envSlug string, days int, cutoff, horizon time.Time, flag ...string) map[string]any {
	since := EffectiveSince(days)
	echo := map[string]any{
		"env":           envSlug,
		"since":         since,
		"sinceDays":     days,
		"cutoff":        cutoff.UTC().Format(time.RFC3339),
		"horizon":       since,
		"rollupHorizon": horizon.UTC().Format(time.RFC3339),
	}
	if f := flagOnly(flag); f != "" {
		echo["flag"] = f
	}
	return echo
}

// HorizonFor returns the UTC-midnight split between raw events and
// pre-purge daily rollups: DayBucket(now - rawDays), the same day grain
// the purge rolls up into. Raw events at/after the horizon are live;
// rollup buckets strictly before it are history. Disjoint by
// construction, so a crash-window row present in BOTH sources is still
// counted once (plus approximate:true marks the taint).
func HorizonFor(now time.Time) time.Time {
	return purge.DayBucket(now.AddDate(0, 0, -retention.GetConfig().RawDays))
}

// EventCutoffFor clamps the raw-events window to the live side of the
// horizon: the later of the ?since= cutoff and the horizon. Short
// windows (<=30d) are untouched; long windows stop at the horizon so
// purged history comes from rollups exactly once.
func EventCutoffFor(cutoff, horizon time.Time) time.Time {
	if cutoff.After(horizon) {
		return cutoff
	}
	return horizon
}

// AggregateRollups folds event_daily buckets into Stats purely (no I/O).
// Filter order mirrors Aggregate: env match -> optional flag match
// (empty filter = env-wide) -> day-grain cutoff
// (day >= cutoffDay kept, exactly-at-cutoff kept, zero-day skipped) ->
// horizon (day < horizon; the boundary day stays raw-side) -> counts
// (fetches+exposures added, perVariant over exposures only, verbatim
// variant incl "", perVersion over fetches only by stored version).
func AggregateRollups(buckets []purge.Rollup, envID string, cutoffDay, horizon time.Time, flagFilter ...string) Stats {
	out := Stats{PerVariant: map[string]int{}, PerVersion: map[int]int{}}
	flag := flagOnly(flagFilter)
	for _, b := range buckets {
		if b.EnvID != envID {
			continue
		}
		if flag != "" && b.Flag != flag {
			continue
		}
		if b.Day.IsZero() || b.Day.Before(cutoffDay) {
			continue
		}
		if !b.Day.Before(horizon) {
			continue
		}
		out.Fetches += b.Fetches
		out.Exposures += b.Exposures
		if b.Exposures > 0 {
			out.PerVariant[b.Variant] += b.Exposures
		}
		if b.Fetches > 0 {
			out.PerVersion[b.Version] += b.Fetches
		}
	}
	return out
}

// ApproximateFor reports whether the merged window is approximate:
// true as soon as any rollup day contributes (rollup counters can carry
// a crash-window overshoot per the purge CRASH SEMANTIC, so merged
// windows must never claim exactness). Events-only windows stay exact.
func ApproximateFor(rollupsTotal int) bool {
	return rollupsTotal > 0
}

// MergeStats folds the rollup aggregate into the events aggregate. Both
// inputs are already windowed to disjoint sides of the horizon, so this
// is a plain sum; perVariant keys merge verbatim ("" included).
func MergeStats(events, rollups Stats) Stats {
	out := Stats{Fetches: events.Fetches + rollups.Fetches, Exposures: events.Exposures + rollups.Exposures, PerVariant: map[string]int{}, PerVersion: map[int]int{}}
	for v, n := range events.PerVariant {
		out.PerVariant[v] += n
	}
	for v, n := range rollups.PerVariant {
		out.PerVariant[v] += n
	}
	for v, n := range events.PerVersion {
		out.PerVersion[v] += n
	}
	for v, n := range rollups.PerVersion {
		out.PerVersion[v] += n
	}
	return out
}

// Aggregate folds rows into Stats purely (no I/O). Filters: envID must
// match; optional flagFilter scopes to one verbatim flag (empty = env-wide);
// rows with ts before cutoff (or zero ts, which proves no
// recency) are excluded. PerVariant counts exposures only.
func Aggregate(rows []EventRow, envID string, cutoff time.Time, flagFilter ...string) Stats {
	out := Stats{PerVariant: map[string]int{}, PerVersion: map[int]int{}}
	flag := flagOnly(flagFilter)
	for _, r := range rows {
		if r.EnvID != envID {
			continue
		}
		if flag != "" && r.Flag != flag {
			continue
		}
		if r.Ts.IsZero() || r.Ts.Before(cutoff) {
			continue
		}
		switch r.Kind {
		case "fetch":
			out.Fetches++
			out.PerVersion[r.Version]++
		case "exposure":
			out.Exposures++
			out.PerVariant[r.Variant]++
		}
	}
	return out
}

// FlagFound reports whether any in-window row or bucket carries flag:
// same env + window predicates as Aggregate/AggregateRollups, kind-agnostic
// (any row counts, not just fetch/exposure). The handler uses it for the
// flagFound verdict on ?flag= requests: true when the flag appears anywhere
// in the window, false for unknown flags (which then answer zeros).
func FlagFound(rows []EventRow, buckets []purge.Rollup, envID string, eventCutoff, cutoffDay, horizon time.Time, flag string) bool {
	for _, r := range rows {
		if r.EnvID != envID || r.Flag != flag {
			continue
		}
		if r.Ts.IsZero() || r.Ts.Before(eventCutoff) {
			continue
		}
		return true
	}
	for _, b := range buckets {
		if b.EnvID != envID || b.Flag != flag {
			continue
		}
		if b.Day.IsZero() || b.Day.Before(cutoffDay) {
			continue
		}
		if !b.Day.Before(horizon) {
			continue
		}
		return true
	}
	return false
}

// resolveFlagID maps a verbatim ?flag= key to its stored flag record ID,
// scoped to the stats env's project (key + project, never global key).
// Events and event_daily carry the flag relation ID, never the key, so
// callers must match rows against the returned ID. ok=false reports an
// unknown key, which answers zeros with flagFound:false (never 404).
func resolveFlagID(app core.App, projectID, key string) (id string, ok bool, err error) {
	recs, err := app.FindRecordsByFilter("flags", "project = {:project} && key = {:key}", "", 1, 0, dbx.Params{"project": projectID, "key": key})
	if err != nil {
		return "", false, err
	}
	if len(recs) == 0 {
		return "", false, nil
	}
	return recs[0].Id, true, nil
}

// Register mounts the admin stats route (superuser-only; SDK keys -> 401).
func Register(se *core.ServeEvent) {
	se.Router.GET("/api/v1/admin/env/{env}/stats", getStats).Bind(apis.RequireSuperuserAuth())
}

// Order: 401 (superuser, via middleware) -> 404 (unknown env slug) ->
// 400 (ambiguous slug without ?project=, malformed since, or over-long
// flag) -> 200 (env-wide counts, or per-flag counts with flagFound when
// ?flag= is set).
// Uses re.App for every request-scoped lookup (never a captured app).
func getStats(re *core.RequestEvent) error {
	security.SetHeaders(re)
	slug := re.Request.PathValue("env")
	// ?project= disambiguates a slug shared by several projects;
	// unambiguous slugs keep working without it.
	env, err := envresolve.Resolve(re.App, slug, re.Request.URL.Query().Get("project"))
	if err != nil {
		return envresolve.ToRequestError(re, err)
	}

	days, perr := ParseSince(re.Request.URL.Query().Get("since"))
	if perr != nil {
		return re.BadRequestError(perr.Error(), nil)
	}
	flag, ferr := ParseFlag(re.Request.URL.Query().Get("flag"))
	if ferr != nil {
		return re.BadRequestError(ferr.Error(), nil)
	}
	// Resolve the verbatim ?flag= key once per request to the stored
	// flag record ID scoped to the env's project. flag stays the
	// verbatim request value for echo; flagFilter is the resolved ID
	// passed to every aggregate. Unknown keys skip the row loads and
	// fall through on empty inputs: zeros with flagFound:false.
	flagFilter := ""
	flagResolved := true
	if flag != "" {
		id, ok, rerr := resolveFlagID(re.App, env.GetString("project"), flag)
		if rerr != nil {
			return rerr
		}
		if !ok {
			flagResolved = false
		} else {
			flagFilter = id
		}
	}
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	horizon := HorizonFor(now)
	cutoffDay := purge.DayBucket(cutoff)

	eventCutoff := EventCutoffFor(cutoff, horizon)
	var rows []EventRow
	var buckets []purge.Rollup
	if flagResolved {
		var err error
		rows, err = loadRows(re.App, env.Id, eventCutoff)
		if err != nil {
			return err
		}
		buckets, err = loadRollups(re.App, env.Id, cutoffDay, horizon)
		if err != nil {
			return err
		}
	}
	stEvents := Aggregate(rows, env.Id, eventCutoff, flagFilter)
	stRollups := AggregateRollups(buckets, env.Id, cutoffDay, horizon, flagFilter)
	st := MergeStats(stEvents, stRollups)

	eventsTotal := TotalFor(stEvents)
	rollupsTotal := TotalFor(stRollups)
	approximate := ApproximateFor(rollupsTotal)

	version, err := releases.MaxVersionForEnv(re.App, env.Id)
	if err != nil {
		return err
	}

	rates := RatesFor(st)
	total := TotalFor(st)
	echo := EchoFor(slug, days, cutoff, horizon, flag)

	// Series window matches the totals window: every counted row lands on
	// exactly one series day, so series sums equal totals (scoped to the
	// flag too when ?flag= is set).
	daysList := SeriesDays(cutoffDay, now)
	evMap := BuildEventSeries(rows, env.Id, eventCutoff, flagFilter)
	roMap := BuildRollupSeries(buckets, env.Id, cutoffDay, horizon, flagFilter)
	series := MergeSeries(evMap, roMap, daysList)

	resp := map[string]any{
		"fetches":     st.Fetches,
		"exposures":   st.Exposures,
		"perVariant":  st.PerVariant,
		"perVersion":  st.PerVersion,
		"version":     version,
		"echo":        echo,
		"total":       total,
		"rates":       rates,
		"sources":     map[string]any{"events": eventsTotal, "rollups": rollupsTotal},
		"approximate": approximate,
		"series":      series,
	}
	if flag != "" {
		resp["flagFound"] = FlagFound(rows, buckets, env.Id, eventCutoff, cutoffDay, horizon, flagFilter)
	}
	return re.JSON(http.StatusOK, resp)
}

// dateParam formats t for indexed Date/DateTime comparisons: PocketBase
// stores dates in types.DefaultDateLayout, which sorts lexicographically
// in chronological order, so >= / < on the formatted string matches the
// in-Go time comparisons exactly (truncation only ever loosens the DB
// prefilter to a superset the frozen Aggregate narrows back down).
func dateParam(t time.Time) string {
	return t.UTC().Format(types.DefaultDateLayout)
}

// loadRollups projects the event_daily collection into purge.Rollup
// buckets by indexed filter: env equality and the day window
// cutoffDay <= day < horizon (history-side of the purge split; the
// boundary day stays raw-side). userHash never exists on this
// collection (counts only).
func loadRollups(app core.App, envID string, cutoffDay, horizon time.Time) ([]purge.Rollup, error) {
	filter := "env = {:env} && day >= {:cutoffDay} && day < {:horizon}"
	params := dbx.Params{
		"env":       envID,
		"cutoffDay": dateParam(cutoffDay),
		"horizon":   dateParam(horizon),
	}
	recs, err := app.FindRecordsByFilter("event_daily", filter, "", 0, 0, params)
	if err != nil {
		return nil, err
	}
	out := make([]purge.Rollup, 0, len(recs))
	for _, r := range recs {
		out = append(out, purge.Rollup{
			Day:       r.GetDateTime("day").Time(),
			EnvID:     r.GetString("env"),
			Flag:      r.GetString("flag"),
			Variant:   r.GetString("variant"),
			Version:   r.GetInt("version"),
			Fetches:   r.GetInt("fetches"),
			Exposures: r.GetInt("exposures"),
		})
	}
	return out, nil
}

// loadRows projects the events collection into EventRows by indexed
// filter: env equality and ts >= cutoff (the live side of the purge
// split; zero-ts rows sort below any cutoff and drop out in the DB, as
// in Aggregate). userHash is deliberately never read — aggregate counts
// only.
func loadRows(app core.App, envID string, cutoff time.Time) ([]EventRow, error) {
	filter := "env = {:env} && ts >= {:cutoff}"
	params := dbx.Params{
		"env":    envID,
		"cutoff": dateParam(cutoff),
	}
	recs, err := app.FindRecordsByFilter("events", filter, "", 0, 0, params)
	if err != nil {
		return nil, err
	}
	rows := make([]EventRow, 0, len(recs))
	for _, r := range recs {
		rows = append(rows, EventRow{
			EnvID:   r.GetString("env"),
			Flag:    r.GetString("flag"),
			Kind:    r.GetString("kind"),
			Variant: r.GetString("variant"),
			Version: r.GetInt("version"),
			Ts:      r.GetDateTime("ts").Time(),
		})
	}
	return rows, nil
}
