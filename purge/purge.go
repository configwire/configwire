// Package purge enforces the compliance retention policy:
// raw events die at 30d, daily aggregates live to 90d.
//
// Rollup-before-delete runs in the SAME operation: rows strictly older
// than the cutoff are first aggregated into event_daily upserts keyed by
// (day, env, flag, variant, version), then the raw rows are deleted. Stale
// event_daily rows older than 90d (cutoff minus RollupRetentionDays minus
// RawRetentionDays) are deleted too. Raw releases/rollups collections are
// never touched — events only.
//
// CRASH SEMANTIC: each page commits atomically in its own short
// transaction — raw pages as (rollup upserts of that page's rows +
// deletes of those rows), stale-rollup pages as (deletes of that page's
// rows) — so a crash or a mid-run failure can neither lose data nor
// double-count: the aborted page rolls back whole (its rollup never
// becomes visible without its deletes), committed pages stay on disk, and
// a re-run walks past them via the same (ts,id)/(day,id) cursor and
// finishes the rest. Already-rolled rows are gone, so counters never
// double-add. Rollup-before-delete order is preserved inside every page
// transaction. The (day,env,flag,variant,version) KEY stays idempotent
// (one row per key, never duplicates); re-running the purge converges
// (second run finds nothing to delete). On error PurgeOlderThan returns
// (0, err) even though earlier pages may already be committed — rerun to
// converge.
//
// DELETION NOTE (no GDPR export UI by design): purged raw rows carry only
// userHash (never raw user ids — see ingest), and event_daily rows carry
// aggregate counts only, so no PII survives past the raw window.
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

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	// RawRetentionDays bounds raw event lifetime before rollup-then-delete.
	RawRetentionDays = 30
	// RollupRetentionDays bounds daily aggregate lifetime.
	RollupRetentionDays = 90
	// rollupExtraDays derives the event_daily cutoff from the raw-events
	// cutoff passed to PurgeOlderThan: 90d - 30d = 60d further back.
	rollupExtraDays = RollupRetentionDays - RawRetentionDays
	// purgePageSize bounds one cursor page: one page = one short
	// transaction, so a 500k-row run holds at most this many rows in heap
	// and never holds the write lock for the whole run.
	purgePageSize = 2000
)

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

// eventRangeExpr is the INDEXED prefilter for the raw-events delete set
// (uses the events ts range; no full-table scan). It is a conservative
// SUPERSET of ShouldDelete(ts, cutoff): storage truncates to milliseconds,
// so a same-millisecond edge row compares equal to the truncated cutoff in
// SQL while still being strictly older in full precision. Callers apply
// ShouldDelete on the read-back rows for the exact verdict — the retention
// predicate itself is unchanged (FROZEN), so the deletion set is
// bit-for-bit identical to the old full scan.
//
// The paged path (eventPageParams + pageFilter, LIMIT purgePageSize) is the
// string-filter twin of this expression with the same superset contract;
// this dbx form is kept as the contract reference.
func eventRangeExpr(cutoff time.Time) dbx.Expression {
	floor := cutoff.UTC().Truncate(time.Millisecond)
	return dbx.NewExp("ts != '' AND ts <= {:cutoff}", dbx.Params{"cutoff": dateParam(floor)})
}

// rollupRangeExpr is the INDEXED prefilter for stale event_daily rows
// (uses the event_daily(day) index). Same superset contract as
// eventRangeExpr: callers re-apply the exact non-zero strict-before
// verdict, so the deletion set matches the old full scan exactly.
//
// The paged path (dailyPageParams + pageFilter, LIMIT purgePageSize) is the
// string-filter twin of this expression with the same superset contract;
// this dbx form is kept as the contract reference.
func rollupRangeExpr(rollupCutoff time.Time) dbx.Expression {
	floor := rollupCutoff.UTC().Truncate(time.Millisecond)
	return dbx.NewExp("day != '' AND day <= {:rc}", dbx.Params{"rc": dateParam(floor)})
}

// emptyOrNull matches the old in-Go GetString equality exactly: PocketBase
// reads both ” and NULL back as "", so an empty bucket value must match
// either storage (an unset env relation may persist as ” or NULL).
func emptyOrNull(col, val string) dbx.Expression {
	if val == "" {
		return dbx.Or(dbx.HashExp{col: ""}, dbx.NewExp(col+" IS NULL"))
	}
	return dbx.HashExp{col: val}
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
// page: the string-filter twin of eventRangeExpr (same superset contract —
// callers still re-apply ShouldDelete per row, so the deletion set is
// bit-for-bit identical to the old full scan).
func eventPageParams(cutoff time.Time) (string, dbx.Params) {
	floor := cutoff.UTC().Truncate(time.Millisecond)
	return "ts != '' && ts <= {:cutoff}", dbx.Params{"cutoff": dateParam(floor)}
}

// dailyPageParams builds the base indexed filter params for one
// event_daily page: the string-filter twin of rollupRangeExpr.
func dailyPageParams(rollupCutoff time.Time) (string, dbx.Params) {
	floor := rollupCutoff.UTC().Truncate(time.Millisecond)
	return "day != '' && day <= {:rc}", dbx.Params{"rc": dateParam(floor)}
}

// PurgeOlderThan rolls raw events with ts strictly before cutoff into
// event_daily upserts, deletes those raw rows, then deletes event_daily
// rows with day strictly before cutoff minus 60d (i.e. older than 90d
// when cutoff is now-30d). It returns the total rows deleted
// (raw events + stale rollups).
//
// Raw reads are cursor-paged over the indexed candidate set ordered by
// (ts, id) with LIMIT purgePageSize: each page is rolled up (that page's
// rows only, rollup-before-delete) and deleted in its own transaction, so
// heap stays flat and the write lock is held per page, not per run. The
// per-row ShouldDelete recheck runs on every row exactly as before, so the
// delete set is unchanged; cutoff/retention math is unchanged.
//
// Crash semantic: per-page atomic + rerun converges. A mid-run crash leaves
// committed pages on disk (rollup + deletes together, never one without
// the other — the aborted page rolls back whole); the next run walks past
// the survivors via the same cursor and finishes them, and already-rolled
// rows are gone so counters never double-add. On error this returns
// (0, err) even though earlier pages may already be committed — rerun to
// converge. (Package-level CRASH SEMANTIC note rewrite rides with todo 3.)
func PurgeOlderThan(app core.App, cutoff time.Time) (deleted int, err error) {
	n := 0
	base, _ := eventPageParams(cutoff)
	cutoffParam := dateParam(cutoff.UTC().Truncate(time.Millisecond))
	var cursorVal, cursorID string
	hasCursor := false
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
		var doomedRows []EventRow
		for _, r := range page {
			ts := r.GetDateTime("ts").Time()
			if !ShouldDelete(ts, cutoff) {
				continue
			}
			doomed = append(doomed, r)
			doomedRows = append(doomedRows, EventRow{
				EnvID:   r.GetString("env"),
				Flag:    r.GetString("flag"),
				Kind:    r.GetString("kind"),
				Variant: r.GetString("variant"),
				Version: r.GetInt("version"),
				Ts:      ts,
			})
		}
		last := page[len(page)-1]
		cursorVal = dateParam(last.GetDateTime("ts").Time())
		cursorID = last.Id
		hasCursor = true
		if len(doomed) > 0 {
			if err := app.RunInTransaction(func(txApp core.App) error {
				// Rollup BEFORE delete: no data-loss window beyond a crash
				// (the aborted page rolls back whole — see Crash semantic).
				// applyRollupBatch runs inside this page txn: its bucket
				// count is bounded by the page's row count (<= purgePageSize),
				// so the txn is never widened beyond the page.
				if err := applyRollupBatch(txApp, BuildRollups(doomedRows)); err != nil {
					return err
				}
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
	rollupCutoff := cutoff.AddDate(0, 0, -rollupExtraDays)

	// Stale aggregates: event_daily rows older than 90d die too, paged by
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

// CountOlderThan reports how many rows PurgeOlderThan would delete with
// the same cutoff, with zero writes (dry-run path). It walks the same
// indexed candidate sets with the same (ts,id)/(day,id) cursor paging and
// applies the same exact predicates, so dry-count == live-deleted
// structurally — and it never loads a whole collection into heap.
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
	rollupCutoff := cutoff.AddDate(0, 0, -rollupExtraDays)
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

// upsertRollups adds buckets in purgePageSize sub-batches, each in its own
// transaction, so a high-cardinality call never lands in one giant write
// set. Callers already inside a page transaction use applyRollupBatch
// directly so the page txn is never widened beyond the page.
func upsertRollups(app core.App, buckets []Rollup) error {
	for start := 0; start < len(buckets); start += purgePageSize {
		end := start + purgePageSize
		if end > len(buckets) {
			end = len(buckets)
		}
		chunk := buckets[start:end]
		if err := app.RunInTransaction(func(txApp core.App) error {
			return applyRollupBatch(txApp, chunk)
		}); err != nil {
			return err
		}
	}
	return nil
}

// applyRollupBatch adds each bucket's counts onto the matching event_daily
// row — found per bucket with an indexed find on (day,env,flag,variant,version)
// (day as a [midnight,midnight+24h) range, which is exactly the old
// Format("2006-01-02") equality for any stored time-of-day; env and flag match
// ”-or-NULL exactly like the old GetString comparison) — or creates the
// row when absent. Re-running with the same buckets converges to one row
// per key — the upsert key is idempotent even though counters add (see
// CRASH SEMANTIC). Runs in the caller's transaction scope (a page txn via
// PurgeOlderThan, or one sub-batch txn via upsertRollups) — it never opens
// its own transaction, so it never widens the write set.
func applyRollupBatch(app core.App, buckets []Rollup) error {
	if len(buckets) == 0 {
		return nil
	}
	col, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		return err
	}
	// The flag column is restored by migration; on a pre-restore DB it is
	// absent, in which case every projected flag is "" and the predicate
	// is a no-op — skip it so the upsert keeps working there too.
	hasFlagField := col.Fields.GetByName("flag") != nil
	for _, b := range buckets {
		dayStart := DayBucket(b.Day)
		dayEnd := dayStart.AddDate(0, 0, 1)
		filter := []dbx.Expression{
			dbx.NewExp("day >= {:d0} AND day < {:d1}", dbx.Params{
				"d0": dateParam(dayStart),
				"d1": dateParam(dayEnd),
			}),
			emptyOrNull("env", b.EnvID),
			dbx.HashExp{"variant": b.Variant},
		}
		if hasFlagField {
			filter = append(filter, emptyOrNull("flag", b.Flag))
		}
		recs, err := app.FindAllRecords("event_daily", filter...)
		if err != nil {
			return err
		}
		var match *core.Record
		for _, r := range recs {
			if r.GetInt("version") == b.Version {
				match = r
				break
			}
		}
		hasVersionField := col.Fields.GetByName("version") != nil
		if match == nil {
			match = core.NewRecord(col)
			match.Set("day", b.Day)
			if b.EnvID != "" {
				match.Set("env", b.EnvID)
			}
			if hasFlagField && b.Flag != "" {
				match.Set("flag", b.Flag)
			}
			match.Set("variant", b.Variant)
			if hasVersionField {
				match.Set("version", b.Version)
			}
			match.Set("fetches", b.Fetches)
			match.Set("exposures", b.Exposures)
			if err := app.Save(match); err != nil {
				return err
			}
			continue
		}
		match.Set("fetches", match.GetInt("fetches")+b.Fetches)
		match.Set("exposures", match.GetInt("exposures")+b.Exposures)
		if err := app.Save(match); err != nil {
			return err
		}
	}
	return nil
}

// Register mounts POST /api/v1/admin/maintenance/purge (superuser-only;
// ?dry=1 reports without writing) and starts the daily 24h ticker that
// purges raw events older than RawRetentionDays. Uses re.App / se.App
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
		cutoff := time.Now().UTC().AddDate(0, 0, -RawRetentionDays)
		n, err := PurgeOlderThan(app, cutoff)
		if err != nil {
			log.Printf("purge: %s run failed: %v", tag, err)
			return
		}
		log.Printf("purge: %s run deleted %d rows (cutoff %s)", tag, n, cutoff.Format(time.RFC3339))
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
// ?dry=1 (or true) -> 200 {"deleted":N,"dry":true} with ZERO writes.
// ?dry= absent/0/false -> live purge -> 200 {"deleted":N,"dry":false}.
// Any other dry value (e.g. abc) -> 400. Unauth/SDK-key -> 401 via
// middleware; empty events table -> 200 deleted:0.
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
	cutoff := time.Now().UTC().AddDate(0, 0, -RawRetentionDays)
	if dry {
		n, err := CountOlderThan(re.App, cutoff)
		if err != nil {
			return err
		}
		return re.JSON(http.StatusOK, map[string]any{
			"deleted": n,
			"dry":     true,
			"cutoff":  cutoff.Format(time.RFC3339),
		})
	}
	n, err := PurgeOlderThan(re.App, cutoff)
	if err != nil {
		return err
	}
	log.Printf("purge: manual run deleted %d rows (cutoff %s)", n, cutoff.Format(time.RFC3339))
	return re.JSON(http.StatusOK, map[string]any{
		"deleted": n,
		"dry":     false,
		"cutoff":  cutoff.Format(time.RFC3339),
	})
}
