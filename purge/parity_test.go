// Parity diff-test for todo 9 (purge filtered-query rewrite).
//
// RED-before: TestNoUnfilteredScansOnHotPath FAILS on the pre-fix purge.go
// (bare FindAllRecords scans). GREEN-after: the hot path is indexed range
// queries and every test below passes with byte-identical deletion sets.
//
// The oracle in each DB test is the OLD predicate logic copied here
// (full-scan + ShouldDelete / day-before-rollupCutoff) — the new code must
// produce exactly the same deletion set: strictly-older deleted,
// at-cutoff kept, just-newer kept, zero-ts/zero-day kept.
package purge

import (
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// TestNoUnfilteredScansOnHotPath is the RED-before gate: purge.go must not
// contain bare single-arg FindAllRecords("events") / ("event_daily") scans.
// Filtered calls with dbx range expressions (FindAllRecords(col, expr...),
// CountRecords(col, expr...)) are the indexed path and are allowed.
func TestNoUnfilteredScansOnHotPath(t *testing.T) {
	body, err := os.ReadFile("purge.go")
	if err != nil {
		t.Fatalf("read purge.go: %v", err)
	}
	for _, col := range []string{"events", "event_daily"} {
		bare := regexp.MustCompile(`FindAllRecords\(\s*"` + col + `"\s*\)`)
		if bare.Match(body) {
			t.Fatalf("hot path still full-scans %q via bare FindAllRecords (must be an indexed range query)", col)
		}
	}
}

func parityTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	mkCol := func(name string, fields ...string) {
		t.Helper()
		col := core.NewBaseCollection(name)
		for _, f := range fields {
			if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
				t.Fatalf("add field %s to %s: %v", f, name, err)
			}
		}
		if err := app.Save(col); err != nil {
			t.Fatalf("save collection %s: %v", name, err)
		}
	}
	mkCol("events",
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"date","name":"ts"}`,
	)
	mkCol("event_daily",
		`{"type":"date","name":"day"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"fetches"}`,
		`{"type":"number","name":"exposures"}`,
	)
	return app
}

func seedEvent(t *testing.T, app *tests.TestApp, env, flag, kind, variant string, ts time.Time, setTs bool) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", env)
	rec.Set("flag", flag)
	rec.Set("kind", kind)
	rec.Set("variant", variant)
	if setTs {
		rec.Set("ts", ts)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save event: %v", err)
	}
	return rec.Id
}

func seedDaily(t *testing.T, app *tests.TestApp, day time.Time, setDay bool, env, flag, variant string, fetches, exposures int) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		t.Fatalf("find event_daily: %v", err)
	}
	rec := core.NewRecord(col)
	if setDay {
		rec.Set("day", day)
	}
	rec.Set("env", env)
	rec.Set("flag", flag)
	rec.Set("variant", variant)
	rec.Set("fetches", fetches)
	rec.Set("exposures", exposures)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save daily: %v", err)
	}
	return rec.Id
}

// oracleDeleteSet is the OLD (pre-fix) selection logic, copied verbatim:
// full scan + ShouldDelete for events, full scan + non-zero strict-before
// for event_daily. The new indexed path must return exactly these sets.
func oracleDeleteSet(t *testing.T, app *tests.TestApp, cutoff time.Time) (events map[string]bool, daily map[string]bool) {
	t.Helper()
	events = map[string]bool{}
	recs, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatalf("oracle events scan: %v", err)
	}
	for _, r := range recs {
		if ShouldDelete(r.GetDateTime("ts").Time(), cutoff) {
			events[r.Id] = true
		}
	}
	daily = map[string]bool{}
	rollupCutoff := cutoff.AddDate(0, 0, -(RollupRetentionDays - RawRetentionDays))
	rows, err := app.FindAllRecords("event_daily")
	if err != nil {
		t.Fatalf("oracle daily scan: %v", err)
	}
	for _, r := range rows {
		day := r.GetDateTime("day").Time()
		if !day.IsZero() && day.Before(rollupCutoff) {
			daily[r.Id] = true
		}
	}
	return events, daily
}

func liveIDs(t *testing.T, app *tests.TestApp, col string) map[string]bool {
	t.Helper()
	recs, err := app.FindAllRecords(col)
	if err != nil {
		t.Fatalf("list %s: %v", col, err)
	}
	out := map[string]bool{}
	for _, r := range recs {
		out[r.Id] = true
	}
	return out
}

// TestPurgeMatchesOracleDeletionSet proves the filtered rewrite deletes
// exactly the oracle set: strictly-older (incl. 1ns-before-cutoff and
// unknown kinds) deleted; at-cutoff, just-newer, fresh, and zero-ts kept;
// stale rollups deleted; at-window-edge and zero-day rollups kept.
func TestPurgeMatchesOracleDeletionSet(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) // ms-aligned
	oldDay := DayBucket(cutoff.AddDate(0, 0, -35))

	idOld := seedEvent(t, app, "e1", "f1", "fetch", "", cutoff.AddDate(0, 0, -35), true)
	idOld2 := seedEvent(t, app, "e1", "f1", "exposure", "control", cutoff.AddDate(0, 0, -35).Add(time.Hour), true)
	idEdgeOlder := seedEvent(t, app, "e1", "f1", "fetch", "", cutoff.Add(-time.Nanosecond), true)
	idWeird := seedEvent(t, app, "e1", "f1", "weird", "", cutoff.AddDate(0, 0, -35), true)
	idNoFlag := seedEvent(t, app, "e1", "", "fetch", "", cutoff.AddDate(0, 0, -35), true)
	idAt := seedEvent(t, app, "e1", "f1", "fetch", "", cutoff, true)
	idNewer := seedEvent(t, app, "e1", "f1", "fetch", "", cutoff.Add(time.Hour), true)
	idZero := seedEvent(t, app, "e1", "f1", "fetch", "", time.Time{}, false)
	idFresh := seedEvent(t, app, "e1", "f1", "fetch", "", time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), true)

	rollupCutoff := cutoff.AddDate(0, 0, -(RollupRetentionDays - RawRetentionDays))
	idStale := seedDaily(t, app, rollupCutoff.AddDate(0, 0, -1), true, "e1", "f1", "", 3, 1)
	idEdge := seedDaily(t, app, rollupCutoff, true, "e1", "f1", "", 3, 1)
	idDailyFresh := seedDaily(t, app, cutoff.AddDate(0, 0, -1), true, "e1", "f1", "", 3, 1)
	idDailyZero := seedDaily(t, app, time.Time{}, false, "e1", "f1", "", 3, 1)
	// Pre-existing bucket for the doomed (oldDay,e1,f1,"") key: the purge
	// must ADD onto it, never duplicate it.
	seedDaily(t, app, oldDay, true, "e1", "f1", "", 10, 5)

	wantEvents, wantDaily := oracleDeleteSet(t, app, cutoff)
	for _, id := range []string{idOld, idOld2, idEdgeOlder, idWeird, idNoFlag} {
		if !wantEvents[id] {
			t.Fatalf("oracle must mark %s doomed", id)
		}
	}
	for _, id := range []string{idAt, idNewer, idZero, idFresh} {
		if wantEvents[id] {
			t.Fatalf("oracle must keep %s", id)
		}
	}
	if !wantDaily[idStale] {
		t.Fatalf("oracle must mark stale rollup doomed")
	}
	for _, id := range []string{idEdge, idDailyFresh, idDailyZero} {
		if wantDaily[id] {
			t.Fatalf("oracle must keep rollup %s", id)
		}
	}

	// Dry/live parity: dry-run count == live deleted.
	dry, err := CountOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("CountOlderThan: %v", err)
	}
	if dry != len(wantEvents)+len(wantDaily) {
		t.Fatalf("dry count = %d, want oracle %d+%d", dry, len(wantEvents), len(wantDaily))
	}
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if deleted != dry {
		t.Fatalf("live deleted = %d, dry predicted %d", deleted, dry)
	}

	// Survivors are exactly the complement of the oracle set.
	for id, kept := range liveIDs(t, app, "events") {
		_ = kept
		if wantEvents[id] {
			t.Fatalf("event %s survived but oracle marked doomed", id)
		}
	}
	for _, id := range []string{idAt, idNewer, idZero, idFresh} {
		if !liveIDs(t, app, "events")[id] {
			t.Fatalf("event %s wrongly deleted", id)
		}
	}
	if liveIDs(t, app, "event_daily")[idStale] {
		t.Fatalf("stale rollup survived")
	}

	// Rollup-before-delete: doomed rows landed in event_daily, merged onto
	// the pre-existing bucket (10 fetches + 1 from idOld = 11, exposures 5).
	var matches []*core.Record
	for _, r := range mustAllDaily(t, app) {
		if r.GetDateTime("day").Time().UTC().Format("2006-01-02") == oldDay.Format("2006-01-02") &&
			r.GetString("env") == "e1" && r.GetString("flag") == "f1" && r.GetString("variant") == "" {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly 1 row for the (day,e1,f1,\"\") key, got %d (upsert must find-or-create)", len(matches))
	}
	if got := matches[0].GetInt("fetches"); got != 11 {
		t.Fatalf("merged fetches = %d, want 11 (10 pre-existing + 1 rolled)", got)
	}
	if got := matches[0].GetInt("exposures"); got != 5 {
		t.Fatalf("merged exposures = %d, want 5 (untouched)", got)
	}

	// Re-run converges: crash-semantic second run finds nothing to delete
	// and must NOT double-add counters.
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

func mustAllDaily(t *testing.T, app *tests.TestApp) []*core.Record {
	t.Helper()
	recs, err := app.FindAllRecords("event_daily")
	if err != nil {
		t.Fatalf("list event_daily: %v", err)
	}
	return recs
}

// TestUpsertFindOrCreateIndexed seeds a collision directly against the
// (day,env,flag,variant) key and proves the indexed upsert adds onto the
// existing row instead of inserting a duplicate.
func TestUpsertFindOrCreateIndexed(t *testing.T) {
	app := parityTestApp(t)
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	seedDaily(t, app, day, true, "e9", "f9", "v", 2, 1)
	if err := upsertRollups(app, []Rollup{{
		Day: day, EnvID: "e9", FlagID: "f9", Variant: "v", Fetches: 3, Exposures: 4,
	}}); err != nil {
		t.Fatalf("upsertRollups: %v", err)
	}
	rows := mustAllDaily(t, app)
	if len(rows) != 1 {
		t.Fatalf("upsert created a duplicate row: %d rows for one key", len(rows))
	}
	if rows[0].GetInt("fetches") != 5 || rows[0].GetInt("exposures") != 5 {
		t.Fatalf("upsert = fetches=%d exposures=%d, want 5/5",
			rows[0].GetInt("fetches"), rows[0].GetInt("exposures"))
	}
	// Empty-flag key (unset relation stores ''): found, not duplicated.
	seedDaily(t, app, day, true, "e9", "", "", 1, 0)
	if err := upsertRollups(app, []Rollup{{
		Day: day, EnvID: "e9", Variant: "", Fetches: 2,
	}}); err != nil {
		t.Fatalf("upsertRollups empty-flag: %v", err)
	}
	rows = mustAllDaily(t, app)
	if len(rows) != 2 {
		t.Fatalf("empty-flag upsert duplicated: %d rows, want 2", len(rows))
	}
}
