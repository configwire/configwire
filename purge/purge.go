// Package purge owns the compliance retention policy and the daily
// rollup state. Raw events live up to EffectiveRawDays (30 days by default,
// smaller with the advanced rawDays override); daily aggregates cover any
// additional retention beyond the raw window. When total retention fits
// inside the active raw window, event_daily is intentionally empty and all
// reads are raw-only.
//
// ROLLUP WALK: EnsureRollups advances a persisted high-water mark
// (rollup_state.lastDay) one completed UTC day at a time, from
// now-rollupDays up to the raw-window boundary, i.e. only the days outside
// the raw window. RollupDay rewrites one day's event_daily rows from the raw rows
// still on disk with ABSOLUTE counts (delete the day's buckets, then save
// one row per (env, flag, variant, version)), so re-running a day is
// idempotent. The day's rollup and its lastDay update commit in one
// transaction, so a late row either lands in the rebuilt day or sees the
// advanced mark — never neither.
//
// LATE ROWS: a backdated event whose day is already rolled is folded in
// additively by AddLateRollup, called from the ingest flush inside the same
// transaction as the raw save. Stats reads raw only for the raw window,
// so a completed day's raw rows are never read again; without the inline
// fold a late row would stay invisible until purge deleted it.
//
// A day with no raw rows is a no-op (it never deletes stale buckets on its
// own). Stats reads completed days from event_daily and the raw window from
// raw events.
//
// PURGE: rows strictly older than the cutoff are deleted page by page
// (one short transaction per page, ordered by (ts, id)); stale
// event_daily rows older than rollupDays are deleted the same way
// (ordered by (day, id)). The cutoff is day-granular (RawCutoff: the UTC
// midnight of now-rawDays) so whole UTC days are deleted — the boundary
// day stays fully raw-owned and fully stats-readable (see RawCutoff).
// Before the first page of a doomed day is
// deleted, that day is rolled up with RollupDay unless event_daily
// already has rows for it — this covers backdated/admin rows older than
// the EnsureRollups window without ever adding counts twice. Raw
// releases/rollups collections are never touched — events only.
//
// CRASH SEMANTIC: the EnsureRollups walk commits one day at a time
// (rollup transaction, then the lastDay update), so an interrupted
// backfill resumes at the next uncommitted day and a re-run converges.
// Raw/stale deletes commit per page. A crash between a RollupDay commit
// and its raw-page deletes leaves the rollups on disk; the re-run sees
// existing buckets and deletes without re-adding (counters never
// double-add). On error PurgeOlderThan returns (0, err) even though
// earlier pages may already be committed — rerun to converge. A rollup
// error aborts the run before any delete.
//
// DELETION NOTE (no GDPR export UI by design): purged raw rows carry only
// the bucket/country identity (never raw user ids — see ingest), and
// event_daily rows carry aggregate counts only, so no PII survives past
// the raw window.
//
// SCHEDULING: a 24h process-local ticker + an admin route. No PocketBase
// cron/scheduler dependency is needed on purpose: ConfigWire ships as a
// single binary, and a ticker plus a manually-triggerable route is the
// smallest correct scheduler for a daily retention job.
package purge

import (
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"

	"github.com/configwire/configwire/audit"
	"github.com/configwire/configwire/logstore"
	"github.com/configwire/configwire/retention"
)

const (
	// RawRetentionDays is the default/max raw event lifetime before
	// rollup-then-delete. It matches retention.MaxRawDays.
	RawRetentionDays = 30
	// RollupRetentionDays is the default total retention (raw + daily).
	RollupRetentionDays = 90
	// scrubAfter bounds raw-event identity lifetime independently of the
	// configured raw window: bucket/country are nulled 48h after ts even
	// when raw rows live longer (rollups never need identity).
	scrubAfter = 48 * time.Hour
	// scrubPageSize bounds one identity-scrub cursor page: one page = one
	// short transaction, same heap/lock discipline as purgePageSize.
	scrubPageSize = purgePageSize
	// purgePageSize bounds one cursor page: one page = one short
	// transaction, so a 500k-row run holds at most this many rows in heap
	// and never holds the write lock for the whole run.
	purgePageSize = 2000
	// rollupPageSize bounds one raw-events page read while RollupDay
	// rebuilds a single day; the merged counts live in one map until the
	// day's write transaction.
	rollupPageSize = 500
	// rollupStateCollectionName holds the rollup high-water mark.
	rollupStateCollectionName = "rollup_state"
	// rollupStateKey identifies the singleton state row.
	rollupStateKey = "global"
)

var (
	// rollupMu serializes the EnsureRollups walk inside this process so
	// two callers (boot ticker + manual route) cannot interleave the
	// read-lastDay / rollup / write-lastDay sequence.
	rollupMu sync.Mutex
	// rollupDayFn is the test seam for one day's rollup. It carries no
	// transaction of its own: callers wrap it so the rollup and the state
	// update commit atomically.
	rollupDayFn = rollupDayTx
)

// configuredRawDays returns the active raw-event window from the
// retention singleton (EffectiveRawDays: stored rawDays override when valid,
// otherwise min(total, MaxRawDays)).
func configuredRawDays() int {
	return retention.GetConfig().EffectiveRawDays()
}

// configuredRollupDays returns the active total retention window from
// the retention singleton.
func configuredRollupDays() int {
	return retention.GetConfig().RollupDays
}

// RollupsEnabled reports whether daily rollups are part of the active
// retention policy. When total retention is within the active raw window,
// the rollup set is intentionally empty and stats should read raw-only.
func RollupsEnabled() bool {
	return configuredRollupDays() > configuredRawDays()
}

// RawCutoff maps now to the raw-events purge cutoff: the UTC midnight of
// now-rawDays. The cutoff is day-granular on purpose, matching the rollup
// grain and the stats read split: whole UTC days are deleted, so the
// boundary day (exactly rawDays ago) stays fully raw-owned — rollups are
// never built for it, and stats reads it from raw events until it leaves
// the window entirely. An instant-based cutoff (now - rawDays exact) would
// split the boundary day between "purged" and "raw" at a sub-day grain,
// losing the purged part: those rows are rolled into a whole-day
// event_daily bucket stats never reads (day < horizon fails), and the raw
// side starts at the boundary-day midnight. Whole-day deletion keeps raw
// lifetime at most just under rawDays+1 days.
//
// Exported because the stats read split and its tests must use the exact
// same formula the ticker and manual route purge with.
func RawCutoff(now time.Time) time.Time {
	return DayBucket(now).AddDate(0, 0, -configuredRawDays())
}

// currentRollupExtraDays derives the event_daily cutoff offset from the
// configured windows (rollupDays - rawDays, >= 0 by the retention
// invariant), so the stale-rollup cutoff is always now-rollupDays when
// the raw cutoff is now-rawDays.
func currentRollupExtraDays() int {
	extra := configuredRollupDays() - configuredRawDays()
	if extra < 0 {
		return 0
	}
	return extra
}

// EventRow is the minimal event projection the purge needs — pure and
// DB-free so rollup math is unit-testable on fake rows.
type EventRow struct {
	EnvID   string
	Flag    string // flag record ID ("" when the relation is absent/unset — GetString reads both "" and NULL as "")
	Kind    string // "fetch" | "exposure"
	Variant string // verbatim stored variant ("" possible, counted as-is)
	Version int
	Ts      time.Time
}

// Rollup is one per-(day, env, flag, variant, version) aggregate bucket stored in event_daily.
type Rollup struct {
	Day       time.Time // UTC midnight of the event day
	EnvID     string
	Flag      string
	Variant   string
	Version   int
	Fetches   int
	Exposures int
}

// DayBucket truncates ts to its UTC calendar day (the rollup grain).
func DayBucket(ts time.Time) time.Time {
	y, m, d := ts.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ShouldDelete is the deterministic cutoff rule: strictly-older-than is
// deleted, exactly-at-cutoff is KEPT, and zero-ts rows (corrupt, no day
// bucket provable) are KEPT — never purge what cannot be rolled up.
func ShouldDelete(ts, cutoff time.Time) bool {
	if ts.IsZero() {
		return false
	}
	return ts.Before(cutoff)
}

// BuildRollups folds rows into per-(day,env,flag,variant,version) buckets purely
// (no I/O). Unknown kinds are ignored (same fail-closed posture as
// stats.Aggregate); zero-ts rows are skipped (unkeepable per ShouldDelete
// they are never in the delete set either).
func BuildRollups(rows []EventRow) []Rollup {
	type key struct {
		day     string // YYYY-MM-DD UTC
		env     string
		flag    string
		variant string
		version int
	}
	byKey := map[key]*Rollup{}
	var order []key
	for _, r := range rows {
		if r.Ts.IsZero() {
			continue
		}
		day := DayBucket(r.Ts)
		k := key{day: day.Format("2006-01-02"), env: r.EnvID, flag: r.Flag, variant: r.Variant, version: r.Version}
		b, ok := byKey[k]
		if !ok {
			b = &Rollup{Day: day, EnvID: r.EnvID, Flag: r.Flag, Variant: r.Variant, Version: r.Version}
			byKey[k] = b
			order = append(order, k)
		}
		switch r.Kind {
		case "fetch":
			b.Fetches++
		case "exposure":
			b.Exposures++
		}
	}
	out := make([]Rollup, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// dateParam formats t in the PocketBase DateTime layout so indexed range
// comparisons are lexicographic == chronological (fixed-width UTC).
func dateParam(t time.Time) string {
	return t.UTC().Format(types.DefaultDateLayout)
}

// ShouldScrub is the identity-scrub cutoff rule: rows with ts strictly
// older than the 48h scrub cutoff lose bucket/country; exactly-at-cutoff,
// fresher, and zero-ts rows keep them. Same strict-before shape as
// ShouldDelete, but the effect is nulling (rows survive for rollup), so it
// carries its own name at the call sites.
func ShouldScrub(ts, scrubCutoff time.Time) bool {
	return ShouldDelete(ts, scrubCutoff)
}

// ScrubCutoff maps now to the identity-scrub horizon (now minus 48h),
// independent of the configured raw/rollup windows.
func ScrubCutoff(now time.Time) time.Time {
	return now.Add(-scrubAfter)
}

// needsScrub reports whether r still carries identity worth nulling:
// past the scrub cutoff with at least one of bucket/country set.
// PocketBase reads both "" and NULL back as "", so empty means clean.
func needsScrub(r *core.Record, scrubCutoff time.Time) bool {
	if !ShouldScrub(r.GetDateTime("ts").Time(), scrubCutoff) {
		return false
	}
	return r.GetString("bucket") != "" || r.GetString("country") != ""
}

// ScrubIdentitiesOlderThan nulls bucket/country on events with ts strictly
// before scrubCutoff (usually ScrubCutoff(now)), keeping every row —
// counts, env, kind, variant, version, and ts are untouched so
// later rollups still count the rows. It returns the rows actually
// scrubbed (already-clean rows cost nothing and are not counted, so a
// rerun converges to 0).
//
// Reads page by (ts, id) with LIMIT scrubPageSize over the same indexed
// candidate set as the purge path; each page's nulls commit in one short
// transaction, so a mid-run crash leaves committed pages nulled and a rerun
// walks the same cursor to finish the rest. ts never changes, so in-place
// nulling cannot shift the cursor.
func ScrubIdentitiesOlderThan(app core.App, scrubCutoff time.Time) (scrubbed int, err error) {
	n := 0
	base, _ := eventPageParams(scrubCutoff)
	cutoffParam := dateParam(scrubCutoff.UTC().Truncate(time.Millisecond))
	var cursorVal, cursorID string
	hasCursor := false
	for {
		params := dbx.Params{"cutoff": cutoffParam}
		filter, params := pageFilter(base, params, "ts", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("events", filter, "ts,id", scrubPageSize, 0, params)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		var dirty []*core.Record
		for _, r := range page {
			if needsScrub(r, scrubCutoff) {
				dirty = append(dirty, r)
			}
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("ts").Time())
		cursorID = last.Id
		hasCursor = true
		if len(dirty) > 0 {
			if err := app.RunInTransaction(func(txApp core.App) error {
				for _, r := range dirty {
					r.Set("bucket", "")
					r.Set("country", "")
					if err := txApp.Save(r); err != nil {
						return err
					}
					n++
				}
				return nil
			}); err != nil {
				return 0, err
			}
		}
		if len(page) < scrubPageSize {
			break
		}
	}
	return n, nil
}

// CountScrubOlderThan reports how many rows ScrubIdentitiesOlderThan would
// null with the same cutoff, with zero writes (dry-run path): rows past
// the scrub cutoff that still carry bucket or country. It walks the same
// indexed candidate set with the same (ts, id) cursor paging.
//
// Dry/live delta: scrubbing creates no rows and nulls in place, so live
// always equals dry exactly on a quiescent table — unlike the purge path,
// where just-rolled stale buckets make live exceed dry (see the
// CountOlderThan contract).
func CountScrubOlderThan(app core.App, scrubCutoff time.Time) (int, error) {
	n := 0
	base, _ := eventPageParams(scrubCutoff)
	cutoffParam := dateParam(scrubCutoff.UTC().Truncate(time.Millisecond))
	var cursorVal, cursorID string
	hasCursor := false
	for {
		params := dbx.Params{"cutoff": cutoffParam}
		filter, params := pageFilter(base, params, "ts", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("events", filter, "ts,id", scrubPageSize, 0, params)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			if needsScrub(r, scrubCutoff) {
				n++
			}
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("ts").Time())
		cursorID = last.Id
		hasCursor = true
		if len(page) < scrubPageSize {
			break
		}
	}
	return n, nil
}

// pageFilter appends a keyset cursor clause to a base range filter so a
// LIMIT-paged read walks the candidate set in total (orderCol, id) order
// without OFFSET skips: rows are strictly after the last row of the
// previous page, so concurrent deletes of already-passed rows cannot shift
// later rows out of the walk. id is unique, so (orderCol, id) is total and
// the walk terminates exactly at the end of the set. cursorVal must be the
// dateParam-formatted order-column value of the previous page's last row.
func pageFilter(base string, params dbx.Params, orderCol string, hasCursor bool, cursorVal, cursorID string) (string, dbx.Params) {
	if !hasCursor {
		return base, params
	}
	params["cursor"] = cursorVal
	params["cursorID"] = cursorID
	return base + " && (" + orderCol + " > {:cursor} || (" + orderCol + " = {:cursor} && id > {:cursorID}))", params
}

// eventPageParams builds the base indexed filter params for one raw-events
// page. The filter is a conservative SUPERSET of ShouldDelete(ts, cutoff):
// storage truncates to milliseconds, so a same-millisecond edge row compares
// equal to the truncated cutoff in SQL while still being strictly older in
// full precision. Callers re-apply ShouldDelete per row, so the deletion set
// is bit-for-bit identical to the old full scan.
func eventPageParams(cutoff time.Time) (string, dbx.Params) {
	floor := cutoff.UTC().Truncate(time.Millisecond)
	return "ts != '' && ts <= {:cutoff}", dbx.Params{"cutoff": dateParam(floor)}
}

// dailyPageParams builds the base indexed filter params for one
// event_daily page. Same superset contract as the raw-events filter:
// callers re-apply the exact non-zero strict-before verdict on the
// read-back rows.
func dailyPageParams(rollupCutoff time.Time) (string, dbx.Params) {
	floor := rollupCutoff.UTC().Truncate(time.Millisecond)
	return "day != '' && day <= {:rc}", dbx.Params{"rc": dateParam(floor)}
}

// dayRangeParams builds the indexed [d0, d1) bounds for one UTC day.
func dayRangeParams(day time.Time) (string, dbx.Params) {
	d0 := DayBucket(day)
	d1 := d0.AddDate(0, 0, 1)
	return "ts != '' && ts >= {:d0} && ts < {:d1}", dbx.Params{
		"d0": dateParam(d0),
		"d1": dateParam(d1),
	}
}

// dayHasRollups reports whether event_daily already holds any row for day.
func dayHasRollups(app core.App, day time.Time) (bool, error) {
	d0 := DayBucket(day)
	d1 := d0.AddDate(0, 0, 1)
	params := dbx.Params{"d0": dateParam(d0), "d1": dateParam(d1)}
	recs, err := app.FindRecordsByFilter("event_daily", "day >= {:d0} && day < {:d1}", "", 1, 0, params)
	if err != nil {
		return false, err
	}
	return len(recs) > 0, nil
}

// RollupDay rewrites event_daily for one UTC day from the raw events still
// on disk, inside its own transaction. See rollupDayTx for the semantics.
func RollupDay(app core.App, day time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		return rollupDayFn(txApp, day)
	})
}

// rollupDayTx rewrites event_daily for one UTC day from the raw events still
// on disk. Raw rows are paged by (ts, id) and folded by BuildRollups into a
// map keyed (env, flag, variant, version); the day's write then deletes
// every event_daily row in [day, day+1) and saves one row per bucket with
// ABSOLUTE counts, so a second run produces the same buckets instead of
// doubling them.
//
// A day with no raw rows returns nil before touching event_daily: the caller
// owns stale-bucket deletion, and a no-op day must not erase a valid rollup.
func rollupDayTx(app core.App, day time.Time) error {
	d0 := DayBucket(day)
	d1 := d0.AddDate(0, 0, 1)
	base, params := dayRangeParams(d0)

	type bucketKey struct {
		env     string
		flag    string
		variant string
		version int
	}
	byKey := map[bucketKey]*Rollup{}
	var order []bucketKey
	rawRows := 0
	var cursorVal, cursorID string
	hasCursor := false
	for {
		filter, pageParams := pageFilter(base, params, "ts", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("events", filter, "ts,id", rollupPageSize, 0, pageParams)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		rawRows += len(page)
		rows := make([]EventRow, 0, len(page))
		for _, r := range page {
			rows = append(rows, EventRow{
				EnvID:   r.GetString("env"),
				Flag:    r.GetString("flag"),
				Kind:    r.GetString("kind"),
				Variant: r.GetString("variant"),
				Version: r.GetInt("version"),
				Ts:      r.GetDateTime("ts").Time(),
			})
		}
		for _, b := range BuildRollups(rows) {
			k := bucketKey{env: b.EnvID, flag: b.Flag, variant: b.Variant, version: b.Version}
			agg, ok := byKey[k]
			if !ok {
				copy := b
				byKey[k] = &copy
				order = append(order, k)
				continue
			}
			agg.Fetches += b.Fetches
			agg.Exposures += b.Exposures
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("ts").Time())
		cursorID = last.Id
		hasCursor = true
		if len(page) < rollupPageSize {
			break
		}
	}
	if rawRows == 0 {
		return nil
	}

	col, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		return err
	}
	existing, err := app.FindRecordsByFilter("event_daily", "day >= {:d0} && day < {:d1}", "", 0, 0, dbx.Params{
		"d0": dateParam(d0),
		"d1": dateParam(d1),
	})
	if err != nil {
		return err
	}
	for _, r := range existing {
		if err := app.Delete(r); err != nil {
			return err
		}
	}
	hasFlagField := col.Fields.GetByName("flag") != nil
	hasVersionField := col.Fields.GetByName("version") != nil
	for _, k := range order {
		b := byKey[k]
		rec := core.NewRecord(col)
		rec.Set("day", b.Day)
		if b.EnvID != "" {
			rec.Set("env", b.EnvID)
		}
		if hasFlagField && b.Flag != "" {
			rec.Set("flag", b.Flag)
		}
		rec.Set("variant", b.Variant)
		if hasVersionField {
			rec.Set("version", b.Version)
		}
		rec.Set("fetches", b.Fetches)
		rec.Set("exposures", b.Exposures)
		if err := app.Save(rec); err != nil {
			return err
		}
	}
	return nil
}

// loadRollupState reads the singleton lastDay (zero when absent). A missing
// collection (pre-migration test app) reads as zero so callers fail safe to
// the legacy stats path rather than erroring.
func loadRollupState(app core.App) (time.Time, error) {
	if _, err := app.FindCollectionByNameOrId(rollupStateCollectionName); err != nil {
		return time.Time{}, nil
	}
	recs, err := app.FindRecordsByFilter(rollupStateCollectionName, "key = {:key}", "", 1, 0, dbx.Params{"key": rollupStateKey})
	if err != nil {
		return time.Time{}, err
	}
	if len(recs) == 0 {
		return time.Time{}, nil
	}
	return recs[0].GetDateTime("lastDay").Time(), nil
}

// saveRollupState persists lastDay as the singleton high-water mark.
func saveRollupState(app core.App, lastDay time.Time) error {
	col, err := app.FindCollectionByNameOrId(rollupStateCollectionName)
	if err != nil {
		return err
	}
	recs, err := app.FindRecordsByFilter(rollupStateCollectionName, "key = {:key}", "", 1, 0, dbx.Params{"key": rollupStateKey})
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("key", rollupStateKey)
	if len(recs) > 0 {
		rec = recs[0]
	}
	rec.Set("lastDay", DayBucket(lastDay))
	return app.Save(rec)
}

// AddLateRollup folds one event's counts into event_daily when its day is at
// or before the persisted high-water mark, i.e. when the day is already
// rolled and its raw rows will never be read again. Ingest calls it inside
// the same transaction as the raw save, so a backdated row is either part of
// the day's rebuild or added here — never dropped between the two. Days past
// the mark are left to EnsureRollups; unknown kinds are ignored, matching
// BuildRollups.
func AddLateRollup(app core.App, ev EventRow) error {
	if ev.Ts.IsZero() || (ev.Kind != "fetch" && ev.Kind != "exposure") {
		return nil
	}
	day := DayBucket(ev.Ts)
	last, err := loadRollupState(app)
	if err != nil || last.IsZero() || day.After(DayBucket(last)) {
		return err
	}
	col, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		return err
	}
	filter := "day = {:day}"
	params := dbx.Params{"day": dateParam(day)}
	if ev.EnvID != "" {
		filter += " && env = {:env}"
		params["env"] = ev.EnvID
	}
	recs, err := app.FindRecordsByFilter("event_daily", filter, "", 0, 0, params)
	if err != nil {
		return err
	}
	var rec *core.Record
	for _, r := range recs {
		if r.GetString("env") == ev.EnvID &&
			r.GetString("flag") == ev.Flag &&
			r.GetString("variant") == ev.Variant &&
			r.GetInt("version") == ev.Version {
			rec = r
			break
		}
	}
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("day", day)
		if ev.EnvID != "" {
			rec.Set("env", ev.EnvID)
		}
		if col.Fields.GetByName("flag") != nil && ev.Flag != "" {
			rec.Set("flag", ev.Flag)
		}
		rec.Set("variant", ev.Variant)
		if col.Fields.GetByName("version") != nil {
			rec.Set("version", ev.Version)
		}
	}
	if ev.Kind == "fetch" {
		rec.Set("fetches", rec.GetInt("fetches")+1)
	} else {
		rec.Set("exposures", rec.GetInt("exposures")+1)
	}
	return app.Save(rec)
}

// EnsureRollups advances the persisted rollup high-water mark through every
// completed UTC day in the rollup-only window: from
// max(DayBucket(now-rollupDays), lastDay+1) up to the raw-window boundary
// DayBucket(now-rawDays) where rawDays is the active raw window.
// Days inside the raw window are never rolled because stats reads them
// from raw events. Each day commits its rollup and its lastDay update
// before the next day starts, so an interrupted backfill resumes at the
// first uncommitted day. It is a no-op when rollups are disabled or once
// lastDay reaches the raw-window boundary, and serialized in-process by
// rollupMu. On error the last committed day is returned and the caller
// must abort before deleting anything.
func EnsureRollups(app core.App, now time.Time) (time.Time, error) {
	rollupMu.Lock()
	defer rollupMu.Unlock()

	last, err := loadRollupState(app)
	if err != nil {
		return time.Time{}, err
	}
	rollupDays := configuredRollupDays()
	rawCutoffDay := DayBucket(now.AddDate(0, 0, -configuredRawDays()))
	start := DayBucket(now.AddDate(0, 0, -rollupDays))
	if !last.IsZero() {
		next := DayBucket(last).AddDate(0, 0, 1)
		if next.After(start) {
			start = next
		}
	}
	if !start.Before(rawCutoffDay) {
		return last, nil
	}
	rolled := 0
	for d := start; d.Before(rawCutoffDay); d = d.AddDate(0, 0, 1) {
		if err := app.RunInTransaction(func(txApp core.App) error {
			if err := rollupDayFn(txApp, d); err != nil {
				return err
			}
			return saveRollupState(txApp, d)
		}); err != nil {
			return last, err
		}
		last = d
		rolled++
		if rolled%7 == 0 {
			log.Printf("purge: rollup backfill through %s (%d days)", d.Format("2006-01-02"), rolled)
		}
	}
	return last, nil
}

// RollupsReady reports whether stats may treat every day before today as
// rollup-owned, i.e. read completed days from event_daily instead of raw
// events (lastDay >= yesterday).
//
// The gate is deliberately conservative and must stay that way: under the
// raw-floor walk above, EnsureRollups stops at the raw-window boundary, so
// lastDay never reaches yesterday — while rollups are enabled by the
// current policy, this can only report true for rollup state carried over
// from the pre-raw-floor model (upgrades), where event_daily genuinely
// covers through yesterday. If the gate were widened to "lastDay covers
// the rollup-only window" (the shape a raw-floor walk actually produces),
// stats would read days -30..-1 from rollups that no longer exist and
// silently report zeros for everything except today. When rollups are
// disabled by policy (total retention within the active raw window), it
// reports true so
// stats reads raw-only without treating it as a fallback. Any read error
// reads as not ready, keeping stats on the safe legacy path.
func RollupsReady(app core.App, now time.Time) bool {
	if !RollupsEnabled() {
		return true
	}
	last, err := loadRollupState(app)
	if err != nil {
		return false
	}
	if last.IsZero() {
		return false
	}
	yesterday := DayBucket(now).AddDate(0, 0, -1)
	return !last.Before(yesterday)
}

// PurgeOlderThan deletes raw events with ts strictly before cutoff (rolling
// up each doomed day first), then deletes event_daily rows with day strictly
// before cutoff minus 60d (i.e. older than 90d when cutoff is now-30d). It
// returns the total rows deleted (raw events + stale rollups).
//
// Production callers pass a day-granular cutoff (RawCutoff: the UTC midnight
// of now-rawDays), so whole UTC days are deleted. That keeps the three day
// grains aligned: EnsureRollups rolls exactly the days the purge deletes
// (day < rawCutoffDay == cutoff), and stats' legacy horizon DayBucket(now -
// rawDays) == cutoff splits rollups (day < cutoff) from raw events
// (ts >= cutoff) with no gap and no overlap.
//
// EnsureRollups runs first for the caller's now (cutoff + rawDays, the exact
// inverse of the retention cutoff) and any error aborts with zero deletes.
// Raw reads are cursor-paged over the indexed candidate set ordered by
// (ts, id) with LIMIT purgePageSize; each page deletes in its own short
// transaction. The first page that reaches a doomed day rolls that day up
// first (unless event_daily already has rows for it), so heap stays flat,
// the write lock is held per page, and a backdated day older than the
// EnsureRollups window is never deleted uncounted.
//
// Crash semantic: per-page atomic + rerun converges. A mid-run crash leaves
// committed pages on disk; the next run walks past the survivors via the
// same cursor and finishes them. Rollups already written for a day are never
// re-added, so counters never double-add. On error this returns (0, err)
// even though earlier pages may already be committed — rerun to converge.

func PurgeOlderThan(app core.App, cutoff time.Time) (deleted int, err error) {
	now := cutoff.AddDate(0, 0, configuredRawDays())
	if _, err := EnsureRollups(app, now); err != nil {
		return 0, err
	}

	n := 0
	base, _ := eventPageParams(cutoff)
	cutoffParam := dateParam(cutoff.UTC().Truncate(time.Millisecond))
	var cursorVal, cursorID string
	hasCursor := false
	handledDays := map[string]bool{}
	for {
		params := dbx.Params{"cutoff": cutoffParam}
		filter, params := pageFilter(base, params, "ts", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("events", filter, "ts,id", purgePageSize, 0, params)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		var doomed []*core.Record
		var doomedDays []time.Time
		for _, r := range page {
			ts := r.GetDateTime("ts").Time()
			if !ShouldDelete(ts, cutoff) {
				continue
			}
			doomed = append(doomed, r)
			day := DayBucket(ts)
			key := day.Format("2006-01-02")
			if !handledDays[key] {
				handledDays[key] = true
				doomedDays = append(doomedDays, day)
			}
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("ts").Time())
		cursorID = last.Id
		hasCursor = true
		if len(doomed) > 0 {
			for _, day := range doomedDays {
				exists, err := dayHasRollups(app, day)
				if err != nil {
					return 0, err
				}
				if exists {
					continue
				}
				if err := app.RunInTransaction(func(txApp core.App) error {
					return rollupDayFn(txApp, day)
				}); err != nil {
					return 0, err
				}
			}
			if err := app.RunInTransaction(func(txApp core.App) error {
				for _, r := range doomed {
					if err := txApp.Delete(r); err != nil {
						return err
					}
					n++
				}
				return nil
			}); err != nil {
				return 0, err
			}
		}
		if len(page) < purgePageSize {
			break
		}
	}
	rollupCutoff := cutoff.AddDate(0, 0, -currentRollupExtraDays())

	// Stale aggregates: event_daily rows older than rollupDays die too, paged by
	// (day, id) with LIMIT purgePageSize — the same keyset discipline as
	// the raw path (dailyPageParams + pageFilter), one txn per page, with
	// the per-row day recheck preserved exactly.
	dailyBase, _ := dailyPageParams(rollupCutoff)
	rcParam := dateParam(rollupCutoff.UTC().Truncate(time.Millisecond))
	cursorVal, cursorID = "", ""
	hasCursor = false
	for {
		params := dbx.Params{"rc": rcParam}
		filter, params := pageFilter(dailyBase, params, "day", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("event_daily", filter, "day,id", purgePageSize, 0, params)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		var doomed []*core.Record
		for _, r := range page {
			day := r.GetDateTime("day").Time()
			if !day.IsZero() && day.Before(rollupCutoff) {
				doomed = append(doomed, r)
			}
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("day").Time())
		cursorID = last.Id
		hasCursor = true
		if len(doomed) > 0 {
			pageDeleted := 0
			if err := app.RunInTransaction(func(txApp core.App) error {
				for _, r := range doomed {
					if err := txApp.Delete(r); err != nil {
						return err
					}
					pageDeleted++
				}
				return nil
			}); err != nil {
				return 0, err
			}
			n += pageDeleted
			log.Printf("purge: daily page deleted %d rows (cutoff %s)", pageDeleted, rollupCutoff.Format(time.RFC3339))
		}
		if len(page) < purgePageSize {
			break
		}
	}
	return n, nil
}

// CountOlderThan reports how many PRE-EXISTING rows PurgeOlderThan would
// delete with the same cutoff, with zero writes (dry-run path): doomed raw
// rows (ShouldDelete) plus pre-existing stale event_daily rows (day
// strictly before cutoff minus 60d). It walks the same indexed candidate
// sets with the same (ts,id)/(day,id) cursor paging and applies the same
// exact predicates — and it never loads a whole collection into heap.
//
// Live-run delta: PurgeOlderThan rolls a doomed day up once before its
// first page is deleted (unless the day already has rollups), and its
// trailing phase deletes stale daily rows — including buckets just rolled
// stale in THIS same run. So when doomed raw rows predate the rollup cutoff
// (older than 90d when cutoff is now-30d), live == dry + #new-stale-buckets
// swept same-run; when doomed raw rows fall inside the rollup window their
// buckets stay fresh and live == dry exactly.
func CountOlderThan(app core.App, cutoff time.Time) (int, error) {
	n := 0
	eventBase, _ := eventPageParams(cutoff)
	cutoffParam := dateParam(cutoff.UTC().Truncate(time.Millisecond))
	var cursorVal, cursorID string
	hasCursor := false
	for {
		params := dbx.Params{"cutoff": cutoffParam}
		filter, params := pageFilter(eventBase, params, "ts", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("events", filter, "ts,id", purgePageSize, 0, params)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			if ShouldDelete(r.GetDateTime("ts").Time(), cutoff) {
				n++
			}
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("ts").Time())
		cursorID = last.Id
		hasCursor = true
		if len(page) < purgePageSize {
			break
		}
	}
	rollupCutoff := cutoff.AddDate(0, 0, -currentRollupExtraDays())
	dailyBase, _ := dailyPageParams(rollupCutoff)
	rcParam := dateParam(rollupCutoff.UTC().Truncate(time.Millisecond))
	cursorVal, cursorID = "", ""
	hasCursor = false
	for {
		params := dbx.Params{"rc": rcParam}
		filter, params := pageFilter(dailyBase, params, "day", hasCursor, cursorVal, cursorID)
		page, err := app.FindRecordsByFilter("event_daily", filter, "day,id", purgePageSize, 0, params)
		if err != nil {
			return n, err
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			day := r.GetDateTime("day").Time()
			if !day.IsZero() && day.Before(rollupCutoff) {
				n++
			}
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("day").Time())
		cursorID = last.Id
		hasCursor = true
		if len(page) < purgePageSize {
			break
		}
	}
	return n, nil
}

// Register mounts POST /api/v1/admin/maintenance/purge (superuser-only;
// ?dry=1 reports without writing) and starts the daily 24h ticker that
// purges raw events whose UTC day has fully left the raw window
// (RawCutoff: whole days, see RawCutoff). Uses re.App / se.App
// per request (never a captured stale app beyond the process singleton
// the ticker legitimately owns, same discipline as ingest.Batcher).
// purgeEnabled reports whether the daily ticker runs: CONFIGWIRE_PURGE
// of off/0/false disables it for multi-process deployments where
// exactly one node should purge (two tickers would double-count
// rollups). The manual route stays mounted regardless.
func purgeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CONFIGWIRE_PURGE"))) {
	case "off", "0", "false":
		return false
	default:
		return true
	}
}

func Register(se *core.ServeEvent) {
	se.Router.POST("/api/v1/admin/maintenance/purge", postPurge).Bind(apis.RequireSuperuserAuth())
	if !purgeEnabled() {
		log.Print("purge: daily ticker disabled via CONFIGWIRE_PURGE (manual route still mounted)")
		return
	}
	stop := make(chan struct{})
	var once sync.Once
	se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		once.Do(func() { close(stop) })
		return e.Next()
	})
	go dailyTicker(se.App, stop)
}

// purgeInterval spaces daily runs 23-25h apart so restarts don't
// phase-lock the run time.
func purgeInterval() time.Duration {
	return 23*time.Hour + time.Duration(rand.Int63n(int64(2*time.Hour)))
}

func dailyTicker(app core.App, stop <-chan struct{}) {
	run := func(tag string) {
		now := time.Now().UTC()
		if _, err := EnsureRollups(app, now); err != nil {
			log.Printf("purge: %s rollup run failed: %v", tag, err)
			return
		}
		scrubbed, serr := ScrubIdentitiesOlderThan(app, ScrubCutoff(now))
		if serr != nil {
			log.Printf("purge: %s scrub run failed: %v", tag, serr)
			return
		}
		cutoff := RawCutoff(now)
		n, err := PurgeOlderThan(app, cutoff)
		if err != nil {
			log.Printf("purge: %s run failed: %v", tag, err)
			return
		}
		sl, err := logstore.Purge(app)
		if err != nil {
			log.Printf("purge: %s server-logs run failed: %v", tag, err)
			return
		}
		al, err := audit.Purge(app)
		if err != nil {
			log.Printf("purge: %s audit run failed: %v", tag, err)
			al = 0
		}
		log.Printf("purge: %s run scrubbed %d deleted %d serverLogs %d auditLogs %d (cutoff %s)", tag, scrubbed, n, sl, al, cutoff.Format(time.RFC3339))
	}
	// Boot run first: upgraded DBs with >30d backlog converge immediately
	// instead of waiting a day (manual route stays the alternative).
	run("boot")
	t := time.NewTicker(purgeInterval())
	defer t.Stop()
	// Sequential: a slow run delays (never overlaps) the next tick.
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			run("daily")
		}
	}
}

// postPurge handles POST /api/v1/admin/maintenance/purge.
// ?dry=1 (or true) -> 200 {"deleted":N,"scrubbed":M,"serverLogs":S,"auditLogs":A,"dry":true} with ZERO
// event/rollup/log writes (one purge.run audit row is appended; deleted counts
// doomed raw + stale daily rows at the raw cutoff (day-granular: the UTC
// midnight of now-rawDays, echoed back as "cutoff"),
// scrubbed counts rows still carrying identity past the 48h scrub cutoff,
// serverLogs counts outdated server_logs rows under the live logstore
// window, auditLogs counts outdated audit_log rows under the audit window).
// ?dry= absent/0/false -> live run (scrub 48h identities, then rollup+purge,
// then server-logs and audit purge) -> 200 {"deleted":N,"scrubbed":M,"serverLogs":S,"auditLogs":A,"dry":false}.
// Any other dry value (e.g. abc) -> 400. Unauth/SDK-key -> 401 via
// middleware; empty events table -> 200 deleted:0 scrubbed:0 serverLogs:0 auditLogs:0.
func postPurge(re *core.RequestEvent) error {
	raw := re.Request.URL.Query().Get("dry")
	dry := false
	switch raw {
	case "", "0", "false":
		dry = false
	case "1", "true":
		dry = true
	default:
		return re.BadRequestError("invalid dry: must be 0 or 1.", nil)
	}
	now := time.Now().UTC()
	cutoff := RawCutoff(now)
	scrubCutoff := ScrubCutoff(now)
	if dry {
		n, err := CountOlderThan(re.App, cutoff)
		if err != nil {
			return err
		}
		m, err := CountScrubOlderThan(re.App, scrubCutoff)
		if err != nil {
			return err
		}
		sl, err := logstore.Count(re.App)
		if err != nil {
			return err
		}
		al, err := audit.Count(re.App)
		if err != nil {
			log.Printf("purge: audit count failed: %v", err)
			al = 0
		}
		audit.Record(re, audit.Entry{
			Action: "purge.run", TargetType: "maintenance",
			TargetLabel: "purge", Status: http.StatusOK,
			Detail: map[string]any{
				"dry": true, "deleted": n, "scrubbed": m,
				"serverLogs": sl, "auditLogs": al,
			},
		})
		return re.JSON(http.StatusOK, map[string]any{
			"deleted":    n,
			"scrubbed":   m,
			"serverLogs": sl,
			"auditLogs":  al,
			"dry":        true,
			"cutoff":     cutoff.Format(time.RFC3339),
		})
	}
	m, err := ScrubIdentitiesOlderThan(re.App, scrubCutoff)
	if err != nil {
		return err
	}
	n, err := PurgeOlderThan(re.App, cutoff)
	if err != nil {
		return err
	}
	sl, err := logstore.Purge(re.App)
	if err != nil {
		return err
	}
	al, err := audit.Purge(re.App)
	if err != nil {
		log.Printf("purge: audit run failed: %v", err)
		al = 0
	}
	log.Printf("purge: manual run scrubbed %d deleted %d serverLogs %d auditLogs %d rows (cutoff %s)", m, n, sl, al, cutoff.Format(time.RFC3339))
	audit.Record(re, audit.Entry{
		Action: "purge.run", TargetType: "maintenance",
		TargetLabel: "purge", Status: http.StatusOK,
		Detail: map[string]any{
			"dry": false, "deleted": n, "scrubbed": m,
			"serverLogs": sl, "auditLogs": al,
		},
	})
	return re.JSON(http.StatusOK, map[string]any{
		"deleted":    n,
		"scrubbed":   m,
		"serverLogs": sl,
		"auditLogs":  al,
		"dry":        false,
		"cutoff":     cutoff.Format(time.RFC3339),
	})
}
