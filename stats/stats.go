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
// ENV POLICY: stats are env-wide only; there is no per-key filtering.
// Unknown env slugs -> 404. Auth is superuser-only via
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
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/purge"
	"github.com/configwire/configwire/releases"
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
)

// ParseSince parses the ?since= query value purely (no I/O), so it is
// unit-testable. "" -> DefaultSinceDays. "<N>d" with N >= 1 -> N clamped
// to MaxSinceDays. Anything else (abc, -5d, 0d, 7h, "7") -> error, which
// the handler maps to 400.
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
	if n > MaxSinceDays {
		n = MaxSinceDays
	}
	return n, nil
}

type sinceError struct{ msg string }

func (e *sinceError) Error() string { return e.msg }

func errBadSince() error {
	return &sinceError{msg: "invalid since: must be <N>d (1-90 days), e.g. 7d."}
}

// EventRow is the minimal event projection Aggregate needs — pure and
// DB-free so aggregation is unit-testable on fake rows. Stats aggregate
// env-wide with no flag reads.
type EventRow struct {
	EnvID   string
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
// window after the 90d clamp, cutoff is the UTC RFC3339 window start,
// and rollupHorizon is the UTC RFC3339 midnight that splits raw events
// from pre-purge daily rollups (see HorizonFor). horizon keeps its original
// label meaning ("7d"); rollupHorizon is the additive machine-readable
// split point, so old clients keep parsing horizon untouched.
func EchoFor(envSlug string, days int, cutoff, horizon time.Time) map[string]any {
	since := EffectiveSince(days)
	return map[string]any{
		"env":           envSlug,
		"since":         since,
		"sinceDays":     days,
		"cutoff":        cutoff.UTC().Format(time.RFC3339),
		"horizon":       since,
		"rollupHorizon": horizon.UTC().Format(time.RFC3339),
	}
}

// HorizonFor returns the UTC-midnight split between raw events and
// pre-purge daily rollups: DayBucket(now - RawRetentionDays), the same
// day grain the purge rolls up into. Raw events at/after the horizon
// are live; rollup buckets strictly before it are history. Disjoint by
// construction, so a crash-window row present in BOTH sources is still
// counted once (plus approximate:true marks the taint).
func HorizonFor(now time.Time) time.Time {
	return purge.DayBucket(now.AddDate(0, 0, -purge.RawRetentionDays))
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
// Filter order mirrors Aggregate: env match -> day-grain cutoff
// (day >= cutoffDay kept, exactly-at-cutoff kept, zero-day skipped) ->
// horizon (day < horizon; the boundary day stays raw-side) -> counts
// (fetches+exposures added, perVariant over exposures only, verbatim
// variant incl "", perVersion over fetches only by stored version).
func AggregateRollups(buckets []purge.Rollup, envID string, cutoffDay, horizon time.Time) Stats {
	out := Stats{PerVariant: map[string]int{}, PerVersion: map[int]int{}}
	for _, b := range buckets {
		if b.EnvID != envID {
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
// match; rows with ts before cutoff (or zero ts, which proves no
// recency) are excluded. All flags in the env count together;
// PerVariant counts exposures only.
func Aggregate(rows []EventRow, envID string, cutoff time.Time) Stats {
	out := Stats{PerVariant: map[string]int{}, PerVersion: map[int]int{}}
	for _, r := range rows {
		if r.EnvID != envID {
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

// Register mounts the admin stats route (superuser-only; SDK keys -> 401).
func Register(se *core.ServeEvent) {
	se.Router.GET("/api/v1/admin/env/{env}/stats", getStats).Bind(apis.RequireSuperuserAuth())
}

// Order: 401 (superuser, via middleware) -> 404 (unknown env slug) ->
// 400 (ambiguous slug without ?project=, or malformed since) -> 200
// (env-wide counts).
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
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	horizon := HorizonFor(now)
	cutoffDay := purge.DayBucket(cutoff)

	eventCutoff := EventCutoffFor(cutoff, horizon)
	rows, err := loadRows(re.App, env.Id, eventCutoff)
	if err != nil {
		return err
	}
	stEvents := Aggregate(rows, env.Id, eventCutoff)

	buckets, err := loadRollups(re.App, env.Id, cutoffDay, horizon)
	if err != nil {
		return err
	}
	stRollups := AggregateRollups(buckets, env.Id, cutoffDay, horizon)
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
	echo := EchoFor(slug, days, cutoff, horizon)

	// Series window matches the totals window: every counted row lands on
	// exactly one series day, so series sums equal totals.
	daysList := SeriesDays(cutoffDay, now)
	evMap := BuildEventSeries(rows, env.Id, eventCutoff)
	roMap := BuildRollupSeries(buckets, env.Id, cutoffDay, horizon)
	series := MergeSeries(evMap, roMap, daysList)

	return re.JSON(http.StatusOK, map[string]any{
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
	})
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
			Kind:    r.GetString("kind"),
			Variant: r.GetString("variant"),
			Version: r.GetInt("version"),
			Ts:      r.GetDateTime("ts").Time(),
		})
	}
	return rows, nil
}
