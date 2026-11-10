// Daily rollup TDD tests: RollupDay absolute-count replacement,
// EnsureRollups resumability, RollupsReady, and PurgeOlderThan's
// rollup-before-delete / abort-on-failure contracts.
package purge

import (
	"errors"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// TestRollupDayReplaceSemantics pins absolute-count replacement: running
// the same day twice produces the same counts (never doubles), and a late
// raw row folded in by a rerun raises the count to the full day total.
func TestRollupDayReplaceSemantics(t *testing.T) {
	app := parityTestApp(t)
	day := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedEvent(t, app, "e1", "fetch", "", day.Add(time.Duration(i)*time.Hour), true)
	}

	if err := RollupDay(app, day); err != nil {
		t.Fatalf("RollupDay: %v", err)
	}
	assertSingleBucket(t, app, day, 3, 0)

	if err := RollupDay(app, day); err != nil {
		t.Fatalf("second RollupDay: %v", err)
	}
	assertSingleBucket(t, app, day, 3, 0)

	seedEvent(t, app, "e1", "exposure", "", day.Add(5*time.Hour), true)
	if err := RollupDay(app, day); err != nil {
		t.Fatalf("third RollupDay (late row): %v", err)
	}
	assertSingleBucket(t, app, day, 3, 1)
}

// TestAddLateRollupFoldsIntoRolledDay pins the backdated-event path: once a
// day is rolled, a later event for that day is added to its bucket, and a
// day past the high-water mark is left to EnsureRollups.
func TestAddLateRollupFoldsIntoRolledDay(t *testing.T) {
	app := parityTestApp(t)
	day := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	seedEvent(t, app, "e1", "fetch", "", day, true)
	if err := RollupDay(app, day); err != nil {
		t.Fatalf("RollupDay: %v", err)
	}
	if err := saveRollupState(app, day); err != nil {
		t.Fatalf("saveRollupState: %v", err)
	}
	assertSingleBucket(t, app, day, 1, 0)

	late := []EventRow{
		{EnvID: "e1", Kind: "fetch", Ts: day.Add(2 * time.Hour)},
		{EnvID: "e1", Kind: "exposure", Variant: "v2", Ts: day.Add(3 * time.Hour)},
	}
	for _, ev := range late {
		if err := AddLateRollup(app, ev); err != nil {
			t.Fatalf("AddLateRollup(%s): %v", ev.Kind, err)
		}
	}
	rows := mustAllDaily(t, app)
	if len(rows) != 2 {
		t.Fatalf("event_daily rows = %d, want 2", len(rows))
	}
	var fetches, exposures int
	for _, r := range rows {
		fetches += r.GetInt("fetches")
		exposures += r.GetInt("exposures")
	}
	if fetches != 2 || exposures != 1 {
		t.Fatalf("folded counts = %d/%d, want 2/1", fetches, exposures)
	}

	if err := AddLateRollup(app, EventRow{EnvID: "e1", Kind: "fetch", Ts: day.AddDate(0, 0, 1)}); err != nil {
		t.Fatalf("AddLateRollup past the mark: %v", err)
	}
	if got := len(mustAllDaily(t, app)); got != 2 {
		t.Fatalf("event_daily rows after a day past the mark = %d, want 2", got)
	}
}

// TestRollupDayEmptyDayLeavesExistingRollup pins the no-op contract: a day
// with no raw rows returns nil and must not delete a valid bucket.
func TestRollupDayEmptyDayLeavesExistingRollup(t *testing.T) {
	app := parityTestApp(t)
	day := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	seedDaily(t, app, day, true, "e1", "", 7, 3)

	if err := RollupDay(app, day); err != nil {
		t.Fatalf("RollupDay empty day: %v", err)
	}
	assertSingleBucket(t, app, day, 7, 3)
}

// TestRollupDayPagesBeyondBoundary pins the page merge: 1200 raw rows
// (three rollupPageSize pages) fold into one bucket with the full count.
func TestRollupDayPagesBeyondBoundary(t *testing.T) {
	app := parityTestApp(t)
	day := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	const n = 1200
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	if err := app.RunInTransaction(func(txApp core.App) error {
		for i := 0; i < n; i++ {
			rec := core.NewRecord(col)
			rec.Set("env", "e1")
			rec.Set("kind", "fetch")
			rec.Set("variant", "")
			rec.Set("ts", day.Add(time.Duration(i)*time.Millisecond))
			if err := txApp.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := RollupDay(app, day); err != nil {
		t.Fatalf("RollupDay: %v", err)
	}
	assertSingleBucket(t, app, day, n, 0)
}

// TestEnsureRollupsAdvancesResumesAndConverges pins the high-water-mark
// walk: it advances day by day, stops on a rollup error at the last
// committed day, resumes at the failed day on the next call, and a third
// call is a no-op.
func TestEnsureRollupsAdvancesResumesAndConverges(t *testing.T) {
	app := parityTestApp(t)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	start := DayBucket(now.AddDate(0, 0, -configuredRawDays()))
	yesterday := DayBucket(now).AddDate(0, 0, -1)

	var calls []time.Time
	rollupDayFn = func(_ core.App, d time.Time) error {
		calls = append(calls, DayBucket(d))
		if len(calls) == 3 {
			return errors.New("ensure-rollups-test: stop on day 3")
		}
		return nil
	}
	t.Cleanup(func() { rollupDayFn = rollupDayTx })

	last, err := EnsureRollups(app, now)
	if err == nil {
		t.Fatal("EnsureRollups with failing day 3 returned nil error")
	}
	if len(calls) != 3 {
		t.Fatalf("rollup calls = %d, want 3 (stop at the failure)", len(calls))
	}
	wantLast := start.AddDate(0, 0, 1)
	if !last.Equal(wantLast) {
		t.Fatalf("returned lastDay = %v, want %v (last committed)", last, wantLast)
	}
	if got := rollupStateDay(t, app); !got.Equal(wantLast) {
		t.Fatalf("persisted lastDay = %v, want %v", got, wantLast)
	}

	calls = nil
	rollupDayFn = func(_ core.App, d time.Time) error {
		calls = append(calls, DayBucket(d))
		return nil
	}
	last, err = EnsureRollups(app, now)
	if err != nil {
		t.Fatalf("resumed EnsureRollups: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("resumed walk made no calls")
	}
	if !calls[0].Equal(start.AddDate(0, 0, 2)) {
		t.Fatalf("resumed at %v, want the failed day %v", calls[0], start.AddDate(0, 0, 2))
	}
	if !last.Equal(yesterday) {
		t.Fatalf("resumed lastDay = %v, want yesterday %v", last, yesterday)
	}
	if got := rollupStateDay(t, app); !got.Equal(yesterday) {
		t.Fatalf("persisted resumed lastDay = %v, want %v", got, yesterday)
	}

	calls = nil
	last, err = EnsureRollups(app, now)
	if err != nil {
		t.Fatalf("idempotent EnsureRollups: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("idempotent walk made %d calls, want 0", len(calls))
	}
	if !last.Equal(yesterday) || !rollupStateDay(t, app).Equal(yesterday) {
		t.Fatalf("idempotent lastDay = %v/%v, want %v", last, rollupStateDay(t, app), yesterday)
	}
}

// TestRollupsReady pins the readiness gate: no state is not ready, yesterday
// (or later) is ready, and an older high-water mark is not.
func TestRollupsReady(t *testing.T) {
	app := parityTestApp(t)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if RollupsReady(app, now) {
		t.Fatal("RollupsReady with no state = true, want false")
	}

	markRollupsReady(t, app, now)
	if !RollupsReady(app, now) {
		t.Fatal("RollupsReady with lastDay=yesterday = false, want true")
	}

	recs, err := app.FindRecordsByFilter("rollup_state", "", "", 1, 0, nil)
	if err != nil || len(recs) == 0 {
		t.Fatalf("read rollup_state: %v", err)
	}
	recs[0].Set("lastDay", DayBucket(now).AddDate(0, 0, -2))
	if err := app.Save(recs[0]); err != nil {
		t.Fatalf("save old lastDay: %v", err)
	}
	if RollupsReady(app, now) {
		t.Fatal("RollupsReady with lastDay=two days ago = true, want false")
	}
}

// TestPurgeOlderThanAbortsOnRollupFailure pins that a rollup error aborts
// before any delete: raw rows and event_daily stay untouched.
func TestPurgeOlderThanAbortsOnRollupFailure(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	now := cutoff.AddDate(0, 0, configuredRawDays())
	markRollupsReady(t, app, now)
	seedEvent(t, app, "e1", "fetch", "", cutoff.AddDate(0, 0, -35), true)

	rollupDayFn = func(_ core.App, _ time.Time) error {
		return errors.New("purge-rollup-test: forced failure")
	}
	t.Cleanup(func() { rollupDayFn = rollupDayTx })

	if _, err := PurgeOlderThan(app, cutoff); err == nil {
		t.Fatal("PurgeOlderThan with failing rollup succeeded, want error")
	}
	if got := len(liveIDs(t, app, "events")); got != 1 {
		t.Fatalf("raw events after aborted purge = %d, want 1 (zero deletes)", got)
	}
	if got := len(liveIDs(t, app, "event_daily")); got != 0 {
		t.Fatalf("event_daily after aborted purge = %d, want 0", got)
	}
}

// TestPurgeOlderThanRollsUpBackdatedDay pins the backdated-day path: a raw
// day older than the EnsureRollups window gets an absolute-count bucket
// before its rows are deleted.
func TestPurgeOlderThanRollsUpBackdatedDay(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	now := cutoff.AddDate(0, 0, configuredRawDays())
	markRollupsReady(t, app, now)

	old := cutoff.AddDate(0, 0, -35)
	seedEvent(t, app, "e1", "fetch", "", old, true)
	seedEvent(t, app, "e1", "fetch", "", old.Add(time.Hour), true)
	seedEvent(t, app, "e1", "exposure", "control", old.Add(2*time.Hour), true)

	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
	rows := mustAllDaily(t, app)
	if len(rows) != 2 {
		t.Fatalf("event_daily rows = %d, want 2 (fetch + exposure buckets)", len(rows))
	}
	var fetches, exposures int
	for _, r := range rows {
		if !DayBucket(r.GetDateTime("day").Time()).Equal(DayBucket(old)) {
			t.Errorf("bucket day = %v, want %v", r.GetDateTime("day").Time(), DayBucket(old))
		}
		fetches += r.GetInt("fetches")
		exposures += r.GetInt("exposures")
	}
	if fetches != 2 || exposures != 1 {
		t.Fatalf("rolled counts = %d/%d, want 2/1", fetches, exposures)
	}
}

func assertSingleBucket(t *testing.T, app *tests.TestApp, day time.Time, fetches, exposures int) {
	t.Helper()
	rows := mustAllDaily(t, app)
	if len(rows) != 1 {
		t.Fatalf("event_daily rows = %d, want exactly 1", len(rows))
	}
	if got := rows[0].GetInt("fetches"); got != fetches {
		t.Fatalf("bucket fetches = %d, want %d", got, fetches)
	}
	if got := rows[0].GetInt("exposures"); got != exposures {
		t.Fatalf("bucket exposures = %d, want %d", got, exposures)
	}
	if got := rows[0].GetDateTime("day").Time(); !DayBucket(got).Equal(DayBucket(day)) {
		t.Fatalf("bucket day = %v, want %v", got, DayBucket(day))
	}
}
