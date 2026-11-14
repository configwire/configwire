// Chunked event_daily TDD tests for todo 3 (ingest-purge-perf).
//
// RED-before: TestEventDailyDeletePathIsBounded FAILS on the pre-fix
// purge.go (the stale event_daily loop uses FindAllRecords with
// rollupRangeExpr inside ONE trailing transaction). GREEN-after: stale
// daily rows page by (day, id) with LIMIT purgePageSize (one txn per page,
// per-row day recheck, partial-progress log per page), boundary-day rows
// are kept, and reruns converge to 0.
package purge

import (
	"errors"
	"os"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// TestEventDailyDeletePathIsBounded is the RED-before gate: the stale
// event_daily delete path must page via FindRecordsByFilter sorted by
// (day, id) — no FindAllRecords load of event_daily may remain on the
// delete path.
func TestEventDailyDeletePathIsBounded(t *testing.T) {
	body, err := os.ReadFile("purge.go")
	if err != nil {
		t.Fatalf("read purge.go: %v", err)
	}
	// The daily delete loop must sort by (day,id) for a stable cursor.
	if !regexp.MustCompile(`"day,id"`).Match(body) {
		t.Fatalf("daily deletes must page sorted by (day,id) for a stable cursor")
	}
	// The daily delete loop must reuse the cursor helpers proven in todo 2.
	if !regexp.MustCompile(`dailyPageParams`).Match(body) {
		t.Fatalf("daily deletes must reuse dailyPageParams (same keyset discipline as the raw path)")
	}
	// The stale-daily delete loop must not load-all: the trailing
	// single-txn FindAllRecords with rollupRangeExpr is the pre-fix shape.
	if regexp.MustCompile(`FindAllRecords\(\s*"event_daily",\s*rollupRangeExpr`).Match(body) {
		t.Fatalf("daily deletes still load-all via FindAllRecords+rollupRangeExpr in one txn (must page by (day,id) with LIMIT)")
	}
}

// TestEventDailyChunked pins stale-daily paging at scale: 2500 stale rows
// (> purgePageSize, so 2 pages: 2000+500) are all deleted, boundary-day /
// zero-day / fresh rows are kept, dry == live, rollup math is untouched
// (no merges happen on this path — counts verified by oracle), and the
// rerun deletes 0.
func TestEventDailyChunked(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) // ms-aligned
	rollupCutoff := cutoff.AddDate(0, 0, -currentRollupExtraDays())

	const nStale = 2500
	staleBase := rollupCutoff.AddDate(0, 0, -10)
	if err := app.RunInTransaction(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("event_daily")
		if err != nil {
			return err
		}
		for i := 0; i < nStale; i++ {
			rec := core.NewRecord(col)
			// Distinct ms-spaced days keep the (day,id) cursor total.
			rec.Set("day", staleBase.Add(time.Duration(i)*time.Millisecond))
			rec.Set("env", "e1")
			rec.Set("variant", "")
			rec.Set("fetches", 1)
			if err := txApp.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed stale daily rows: %v", err)
	}
	idEdge := seedDaily(t, app, rollupCutoff, true, "e1", "", 7, 3)
	idFresh := seedDaily(t, app, cutoff.AddDate(0, 0, -1), true, "e1", "", 7, 3)
	idZero := seedDaily(t, app, time.Time{}, false, "e1", "", 7, 3)

	wantEvents, wantDaily := oracleDeleteSet(t, app, cutoff)
	if len(wantDaily) != nStale {
		t.Fatalf("oracle doomed daily = %d, want %d", len(wantDaily), nStale)
	}
	if len(wantEvents) != 0 {
		t.Fatalf("oracle doomed events = %d, want 0 (none seeded)", len(wantEvents))
	}
	for _, id := range []string{idEdge, idFresh, idZero} {
		if wantDaily[id] {
			t.Fatalf("oracle must keep boundary row %s", id)
		}
	}

	dry, err := CountOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("CountOlderThan: %v", err)
	}
	if dry != nStale {
		t.Fatalf("dry count = %d, want %d", dry, nStale)
	}
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if deleted != dry {
		t.Fatalf("live deleted = %d, dry predicted %d", deleted, dry)
	}

	survivors := liveIDs(t, app, "event_daily")
	if len(survivors) != 3 {
		t.Fatalf("survivors = %d rows, want 3 (edge-day, fresh, zero-day)", len(survivors))
	}
	for _, id := range []string{idEdge, idFresh, idZero} {
		if !survivors[id] {
			t.Fatalf("boundary row %s wrongly deleted", id)
		}
	}
	// Kept rows are byte-untouched (this path deletes only — no merges).
	for _, r := range mustAllDaily(t, app) {
		if got := r.GetInt("fetches"); got != 7 {
			t.Fatalf("kept row fetches = %d, want 7 (untouched)", got)
		}
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

// TestEventDailyConverge pins crash-rerun convergence on the daily path:
// aborting mid-daily-delete (after page 1 commits) leaves partial progress
// on disk; the rerun finishes the rest, and a further run deletes 0. On
// the old single-txn code the abort rolls back everything (0 rows
// deleted), so the partial-progress assertion is the RED gate.
func TestEventDailyConverge(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	rollupCutoff := cutoff.AddDate(0, 0, -currentRollupExtraDays())

	const nStale = 2500
	staleBase := rollupCutoff.AddDate(0, 0, -10)
	if err := app.RunInTransaction(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("event_daily")
		if err != nil {
			return err
		}
		for i := 0; i < nStale; i++ {
			rec := core.NewRecord(col)
			rec.Set("day", staleBase.Add(time.Duration(i)*time.Millisecond))
			rec.Set("env", "e1")
			rec.Set("variant", "")
			rec.Set("fetches", 1)
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
	const failAfter = 2100 // past page 1 (LIMIT 2000), inside page 2
	app.OnRecordDelete("event_daily").BindFunc(func(e *core.RecordEvent) error {
		if failEnabled.Load() && deletes.Add(1) > failAfter {
			return errors.New("purge-daily-converge-test: simulated mid-run crash")
		}
		return e.Next()
	})

	if _, err := PurgeOlderThan(app, cutoff); err == nil {
		t.Fatal("aborted purge succeeded, want simulated crash error")
	}
	remaining := len(liveIDs(t, app, "event_daily"))
	progress := nStale - remaining
	if progress <= 0 || progress >= nStale {
		t.Fatalf("after abort: progress = %d rows committed, want strictly partial (0 < p < %d)", progress, nStale)
	}

	failEnabled.Store(false)
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("rerun PurgeOlderThan: %v", err)
	}
	if deleted != remaining {
		t.Fatalf("rerun deleted = %d, want remainder %d", deleted, remaining)
	}
	deleted3, err := PurgeOlderThan(app, cutoff)
	if err != nil || deleted3 != 0 {
		t.Fatalf("third purge = %d, err=%v; want 0", deleted3, err)
	}
}

// TestEventDailyEmpty pins the empty-daily edge: a purge with no
// event_daily rows deletes raw-only counts and converges immediately.
func TestEventDailyEmpty(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	seedEvent(t, app, "e1", "fetch", "", cutoff.AddDate(0, 0, -35), true)

	dry, err := CountOlderThan(app, cutoff)
	if err != nil || dry != 1 {
		t.Fatalf("dry = %d, err=%v; want 1 (raw only)", dry, err)
	}
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil || deleted != 1 {
		t.Fatalf("live deleted = %d, err=%v; want 1", deleted, err)
	}
	deleted2, err := PurgeOlderThan(app, cutoff)
	if err != nil || deleted2 != 0 {
		t.Fatalf("rerun deleted = %d, err=%v; want 0", deleted2, err)
	}
}
