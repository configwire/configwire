// Chunked-purge TDD tests for todo 2 (ingest-purge-perf).
//
// RED-before: TestPurgeRawPathIsBounded FAILS on the pre-fix purge.go
// (FindAllRecords("events", ...) load-all in PurgeOlderThan/CountOlderThan),
// and TestPurgeConverge FAILS there too (single-txn rollback leaves 0 rows
// deleted after a mid-run abort, so no partial progress converges).
// GREEN-after: cursor-paged reads (LIMIT 2000, sort ts,id) with per-row
// ShouldDelete recheck, one txn per page (rollup-before-delete preserved),
// dry == live on a ~5000-row seed, rerun converges with no double-count.
package purge

import (
	"errors"
	"os"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// TestPurgeRawPathIsBounded is the RED-before gate: no load-all of the raw
// events collection may remain on the purge/dry paths. Paged reads must go
// through FindRecordsByFilter with a limit (sort ts,id); the stale
// event_daily delete loop stays single-shot until todo 3, so filtered
// FindAllRecords on event_daily (upsert + daily delete) remain allowed.
func TestPurgeRawPathIsBounded(t *testing.T) {
	body, err := os.ReadFile("purge.go")
	if err != nil {
		t.Fatalf("read purge.go: %v", err)
	}
	if regexp.MustCompile(`FindAllRecords\(\s*"events"`).Match(body) {
		t.Fatalf("purge raw path still load-alls events via FindAllRecords (must page with FindRecordsByFilter + limit)")
	}
	if !regexp.MustCompile(`FindRecordsByFilter`).Match(body) {
		t.Fatalf("purge.go must page via FindRecordsByFilter (no paged reads found)")
	}
	if !regexp.MustCompile(`"ts,id"`).Match(body) {
		t.Fatalf("paged reads must sort by (ts,id) for a stable cursor")
	}
	// Bare single-arg FindAllRecords (unfiltered full scan) must exist
	// nowhere in purge.go (only tests may full-scan via the oracle).
	for _, col := range []string{"events", "event_daily"} {
		bare := regexp.MustCompile(`FindAllRecords\(\s*"` + col + `"\s*\)`)
		if bare.Match(body) {
			t.Fatalf("unfiltered full scan of %q remains (must be a bounded/paged query)", col)
		}
	}
}

// TestPurgeChunked pins parity at scale: on a ~5000-row seed the chunked
// run deletes exactly the oracle set, dry == live, boundary rows survive,
// rollup totals are single-counted, and the second run deletes 0.
func TestPurgeChunked(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) // ms-aligned
	const nDoomed = 5000
	old := cutoff.AddDate(0, 0, -35)
	if err := app.RunInTransaction(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		for i := 0; i < nDoomed; i++ {
			rec := core.NewRecord(col)
			rec.Set("env", "e1")
			rec.Set("kind", "fetch")
			rec.Set("variant", "")
			// Distinct ms-spaced timestamps keep the (ts,id) cursor total;
			// the 5000ms span never crosses a UTC midnight from noon.
			rec.Set("ts", old.Add(time.Duration(i)*time.Millisecond))
			if err := txApp.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed doomed rows: %v", err)
	}
	idAt := seedEvent(t, app, "e1", "fetch", "", cutoff, true)
	idZero := seedEvent(t, app, "e1", "fetch", "", time.Time{}, false)
	idFresh := seedEvent(t, app, "e1", "fetch", "", cutoff.AddDate(0, 0, 5), true)

	wantEvents, wantDaily := oracleDeleteSet(t, app, cutoff)
	if len(wantEvents) != nDoomed {
		t.Fatalf("oracle doomed events = %d, want %d", len(wantEvents), nDoomed)
	}
	if len(wantDaily) != 0 {
		t.Fatalf("oracle doomed daily = %d, want 0 (none seeded)", len(wantDaily))
	}
	for _, id := range []string{idAt, idZero, idFresh} {
		if wantEvents[id] {
			t.Fatalf("oracle must keep boundary row %s", id)
		}
	}

	dry, err := CountOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("CountOlderThan: %v", err)
	}
	if dry != nDoomed {
		t.Fatalf("dry count = %d, want %d", dry, nDoomed)
	}
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if deleted != dry {
		t.Fatalf("live deleted = %d, dry predicted %d", deleted, dry)
	}

	survivors := liveIDs(t, app, "events")
	if len(survivors) != 3 {
		t.Fatalf("survivors = %d rows, want 3 (at-cutoff, zero-ts, fresh)", len(survivors))
	}
	for _, id := range []string{idAt, idZero, idFresh} {
		if !survivors[id] {
			t.Fatalf("boundary row %s wrongly deleted", id)
		}
	}

	// Rollup-before-delete, single-counted: exactly one bucket row with
	// all 5000 fetches merged onto it.
	oldDay := DayBucket(old)
	rows := mustAllDaily(t, app)
	if len(rows) != 1 {
		t.Fatalf("event_daily rows = %d, want exactly 1 merged bucket", len(rows))
	}
	if got := rows[0].GetInt("fetches"); got != nDoomed {
		t.Fatalf("merged fetches = %d, want %d (single-counted)", got, nDoomed)
	}
	if d := rows[0].GetDateTime("day").Time(); !DayBucket(d).Equal(oldDay) {
		t.Fatalf("bucket day = %v, want %v", d, oldDay)
	}

	deleted2, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("second PurgeOlderThan: %v", err)
	}
	if deleted2 != 0 {
		t.Fatalf("second purge deleted %d, want 0 (converges)", deleted2)
	}
	n2, err := CountOlderThan(app, cutoff)
	if err != nil || n2 != 0 {
		t.Fatalf("second CountOlderThan = %d, err=%v; want 0", n2, err)
	}
}

// TestCountOlderThanChunked pins the dry path at scale plus the empty-table
// edge: dry == live on the ~5000-row seed, and 0/0 on an empty table.
func TestCountOlderThanChunked(t *testing.T) {
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	app := parityTestApp(t)
	old := cutoff.AddDate(0, 0, -35)
	if err := app.RunInTransaction(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		for i := 0; i < 5000; i++ {
			rec := core.NewRecord(col)
			rec.Set("env", "e1")
			rec.Set("kind", "fetch")
			rec.Set("ts", old.Add(time.Duration(i)*time.Millisecond))
			if err := txApp.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dry, err := CountOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("CountOlderThan: %v", err)
	}
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if dry != deleted || dry != 5000 {
		t.Fatalf("dry = %d, live = %d; want both 5000", dry, deleted)
	}

	empty := parityTestApp(t)
	d0, err := CountOlderThan(empty, cutoff)
	if err != nil || d0 != 0 {
		t.Fatalf("empty CountOlderThan = %d, err=%v; want 0", d0, err)
	}
	p0, err := PurgeOlderThan(empty, cutoff)
	if err != nil || p0 != 0 {
		t.Fatalf("empty PurgeOlderThan = %d, err=%v; want 0", p0, err)
	}
}

// TestDryLiveDivergence pins the exact dry/live contract documented on
// CountOlderThan: in-window doomed raw rows roll into FRESH buckets (kept),
// so dry == live exactly; raw rows predating the rollup cutoff roll STALE
// and are swept in the same run, so live == dry + #new-stale-buckets. Both
// cases assert EXACT counts, and each live run converges to 0 on rerun.
func TestDryLiveDivergence(t *testing.T) {
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) // ms-aligned

	// Case A — in-window: 2 raw rows 35d old roll into a FRESH bucket.
	inWindow := parityTestApp(t)
	oldFresh := cutoff.AddDate(0, 0, -35)
	seedEvent(t, inWindow, "e1", "fetch", "", oldFresh, true)
	seedEvent(t, inWindow, "e1", "fetch", "", oldFresh.Add(time.Millisecond), true)
	dryA, err := CountOlderThan(inWindow, cutoff)
	if err != nil {
		t.Fatalf("in-window CountOlderThan: %v", err)
	}
	if dryA != 2 {
		t.Fatalf("in-window dry = %d, want exactly 2 (pre-existing doomed raw rows)", dryA)
	}
	liveA, err := PurgeOlderThan(inWindow, cutoff)
	if err != nil {
		t.Fatalf("in-window PurgeOlderThan: %v", err)
	}
	if liveA != dryA {
		t.Fatalf("in-window live = %d, dry = %d; want live == dry exactly (fresh bucket kept)", liveA, dryA)
	}
	if rows := mustAllDaily(t, inWindow); len(rows) != 1 {
		t.Fatalf("in-window event_daily rows = %d, want exactly 1 (fresh just-rolled bucket kept)", len(rows))
	}
	if rerun, err := PurgeOlderThan(inWindow, cutoff); err != nil || rerun != 0 {
		t.Fatalf("in-window rerun = %d, err=%v; want 0 (converges)", rerun, err)
	}

	// Case B — pre-rollup-cutoff: 2 raw rows 100d old share one bucket, so
	// exactly 1 new stale bucket is swept same-run: live == dry + 1.
	stale := parityTestApp(t)
	oldStale := cutoff.AddDate(0, 0, -100)
	seedEvent(t, stale, "e1", "fetch", "", oldStale, true)
	seedEvent(t, stale, "e1", "fetch", "", oldStale.Add(time.Millisecond), true)
	dryB, err := CountOlderThan(stale, cutoff)
	if err != nil {
		t.Fatalf("stale CountOlderThan: %v", err)
	}
	if dryB != 2 {
		t.Fatalf("stale dry = %d, want exactly 2 (pre-existing doomed raw rows only)", dryB)
	}
	liveB, err := PurgeOlderThan(stale, cutoff)
	if err != nil {
		t.Fatalf("stale PurgeOlderThan: %v", err)
	}
	const wantNewStaleBuckets = 1
	if liveB != dryB+wantNewStaleBuckets {
		t.Fatalf("stale live = %d, dry = %d; want live == dry + %d exactly (just-rolled stale bucket swept same-run)", liveB, dryB, wantNewStaleBuckets)
	}
	if rows := mustAllDaily(t, stale); len(rows) != 0 {
		t.Fatalf("stale event_daily rows = %d, want 0 (just-rolled stale bucket swept same-run)", len(rows))
	}
	if rerun, err := PurgeOlderThan(stale, cutoff); err != nil || rerun != 0 {
		t.Fatalf("stale rerun = %d, err=%v; want 0 (converges)", rerun, err)
	}
}

// TestPurgeConverge pins crash-rerun convergence with per-page atomicity:
// aborting mid-run (after page 1 commits) leaves partial progress on disk;
// the rerun finishes the rest with no rollup double-count, and a further
// run deletes 0. On the old single-txn code the abort rolls back everything
// (0 rows deleted), so the partial-progress assertion is the RED gate.
func TestPurgeConverge(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	const nDoomed = 5000
	old := cutoff.AddDate(0, 0, -35)
	if err := app.RunInTransaction(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		for i := 0; i < nDoomed; i++ {
			rec := core.NewRecord(col)
			rec.Set("env", "e1")
			rec.Set("kind", "fetch")
			rec.Set("ts", old.Add(time.Duration(i)*time.Millisecond))
			if err := txApp.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var deletes atomic.Int64
	var failEnabled atomic.Bool
	failEnabled.Store(true)
	const failAfter = 2500 // past page 1 (LIMIT 2000), inside page 2
	app.OnRecordDelete("events").BindFunc(func(e *core.RecordEvent) error {
		if failEnabled.Load() && deletes.Add(1) > failAfter {
			return errors.New("purge-converge-test: simulated mid-run crash")
		}
		return e.Next()
	})

	if _, err := PurgeOlderThan(app, cutoff); err == nil {
		t.Fatal("aborted purge succeeded, want simulated crash error")
	}
	remaining := len(liveIDs(t, app, "events"))
	progress := nDoomed - remaining
	if progress <= 0 || progress >= nDoomed {
		t.Fatalf("after abort: progress = %d rows committed, want strictly partial (0 < p < %d)", progress, nDoomed)
	}

	// Dry mid-recovery agrees with the live remainder.
	dryMid, err := CountOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("mid-recovery CountOlderThan: %v", err)
	}
	if dryMid != remaining {
		t.Fatalf("mid-recovery dry = %d, live remainder = %d", dryMid, remaining)
	}

	// Rerun converges: deletes exactly the remainder, then 0.
	failEnabled.Store(false)
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("rerun PurgeOlderThan: %v", err)
	}
	if deleted != remaining {
		t.Fatalf("rerun deleted = %d, want remainder %d", deleted, remaining)
	}
	rows := mustAllDaily(t, app)
	if len(rows) != 1 {
		t.Fatalf("event_daily rows = %d, want exactly 1 (no double-count dup)", len(rows))
	}
	if got := rows[0].GetInt("fetches"); got != nDoomed {
		t.Fatalf("converged fetches = %d, want %d (aborted page re-rolled exactly once)", got, nDoomed)
	}
	deleted3, err := PurgeOlderThan(app, cutoff)
	if err != nil || deleted3 != 0 {
		t.Fatalf("third purge = %d, err=%v; want 0", deleted3, err)
	}
}
