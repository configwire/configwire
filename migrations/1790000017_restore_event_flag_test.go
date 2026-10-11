package migrations

// Inverse of 1790000008_drop_event_flag_test.go: proves the restore
// migration re-adds flag attribution on events/event_daily.
//
// tests.NewTestApp boots with every registered migration applied, so the
// app starts POST-restore (flag present); the flaglessFixture rewinds to
// the flagless shape via removeEventFlagDown so restoreEventFlagUp can be
// exercised from the pre-restore state. Reuses the fixture helpers from
// 1790000008_drop_event_flag_test.go (same package): saveProject,
// saveEnv, mustCollection, requireFlagRelation, seedDaily, dailyByID.
// Style follows that file: stdlib testing, Given/When/Then, fresh app per
// test, no sleep, -race/-shuffle safe.

import (
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// flaglessFixture rewinds a fresh (post-restore) TestApp to the flagless
// shape the restore up migration starts from.
func flaglessFixture(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	if err := removeEventFlagDown(app, "events"); err != nil {
		t.Fatalf("fixture removeEventFlagDown events: %v", err)
	}
	if err := removeEventFlagDown(app, "event_daily"); err != nil {
		t.Fatalf("fixture removeEventFlagDown event_daily: %v", err)
	}
	return app
}

// upsertKeyColumns extracts the ordered column list from a PocketBase
// index DDL string, e.g. CREATE UNIQUE INDEX "idx" ON "t" ("a", "b").
func upsertKeyColumns(t *testing.T, ddl string) []string {
	t.Helper()
	start := strings.LastIndex(ddl, "(")
	end := strings.LastIndex(ddl, ")")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("cannot parse index columns from DDL: %q", ddl)
	}
	var cols []string
	for _, part := range strings.Split(ddl[start+1:end], ",") {
		if col := strings.Trim(strings.TrimSpace(part), "\"'`"); col != "" {
			cols = append(cols, col)
		}
	}
	return cols
}

func requireUpsertColumns(t *testing.T, ddl string, want []string) {
	t.Helper()
	got := upsertKeyColumns(t, ddl)
	if len(got) != len(want) {
		t.Fatalf("upsert key columns = %v, want %v (DDL %q)", got, want, ddl)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upsert key columns = %v, want %v (DDL %q)", got, want, ddl)
		}
	}
}

// TestRestoreEventFlagApplied proves the migration is registered: a fresh
// TestApp (all migrations applied) already carries the flag attribution.
func TestRestoreEventFlagApplied(t *testing.T) {
	// Given: a fresh test app with all migrations applied.
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	// Then: flag is present on both collections with the index + key.
	requireFlagRelation(t, app, "events")
	requireFlagRelation(t, app, "event_daily")
	if got := mustCollection(t, app, "events").GetIndex("idx_events_env_flag_ts"); got == "" {
		t.Errorf("events: idx_events_env_flag_ts missing after migrations")
	}
	upsert := mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after migrations")
	}
	if !strings.Contains(strings.ToUpper(upsert), "UNIQUE") {
		t.Errorf("event_daily: upsert key not UNIQUE after migrations: %q", upsert)
	}
	requireUpsertColumns(t, upsert, []string{"day", "env", "flag", "variant", "version"})
}

// TestRestoreEventFlagUp proves up re-adds the flag attribution storage:
// flag fields back on both collections, idx_events_env_flag_ts restored,
// and the daily upsert key rekeyed flag-aware as UNIQUE
// (day, env, flag, variant, version).
func TestRestoreEventFlagUp(t *testing.T) {
	// Given: the flagless schema (post-0008 shape).
	app := flaglessFixture(t)
	if mustCollection(t, app, "events").Fields.GetByName("flag") != nil {
		t.Fatalf("fixture: events flag still present before up")
	}

	// When: the up migration runs on both collections.
	if err := restoreEventFlagUp(app, "events"); err != nil {
		t.Fatalf("restoreEventFlagUp events: %v", err)
	}
	if err := restoreEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("restoreEventFlagUp event_daily: %v", err)
	}

	// Then: flag is back as nullable single cascade-delete relation.
	requireFlagRelation(t, app, "events")
	requireFlagRelation(t, app, "event_daily")

	// Then: the events env/flag/ts index is back.
	if got := mustCollection(t, app, "events").GetIndex("idx_events_env_flag_ts"); got == "" {
		t.Errorf("events: idx_events_env_flag_ts missing after up")
	}

	// Then: the daily upsert key is flag-aware and UNIQUE.
	upsert := mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after up")
	}
	if !strings.Contains(strings.ToUpper(upsert), "UNIQUE") {
		t.Errorf("event_daily: upsert key not UNIQUE after up: %q", upsert)
	}
	requireUpsertColumns(t, upsert, []string{"day", "env", "flag", "variant", "version"})
}

// TestRestoreEventFlagIdempotent proves re-running up is nil-safe and
// that absent collections are skipped instead of erroring.
func TestRestoreEventFlagIdempotent(t *testing.T) {
	// Given: the flagless schema, already migrated once.
	app := flaglessFixture(t)
	if err := restoreEventFlagUp(app, "events"); err != nil {
		t.Fatalf("first restoreEventFlagUp events: %v", err)
	}
	if err := restoreEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("first restoreEventFlagUp event_daily: %v", err)
	}

	// When: up runs a second time.
	if err := restoreEventFlagUp(app, "events"); err != nil {
		t.Fatalf("second restoreEventFlagUp events: %v", err)
	}
	if err := restoreEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("second restoreEventFlagUp event_daily: %v", err)
	}

	// Then: schema stays in the post-up shape.
	requireFlagRelation(t, app, "events")
	requireFlagRelation(t, app, "event_daily")
	if got := mustCollection(t, app, "events").GetIndex("idx_events_env_flag_ts"); got == "" {
		t.Errorf("events: idx_events_env_flag_ts missing after second up")
	}
	upsert := mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after second up")
	}
	requireUpsertColumns(t, upsert, []string{"day", "env", "flag", "variant", "version"})

	// Then: missing collections are nil-safe.
	if err := restoreEventFlagUp(app, "no_such_collection"); err != nil {
		t.Errorf("restoreEventFlagUp missing collection: %v, want nil", err)
	}
	if err := removeEventFlagDown(app, "no_such_collection"); err != nil {
		t.Errorf("removeEventFlagDown missing collection: %v, want nil", err)
	}
}

// TestRemoveEventFlagDownBestEffort proves down removes the attribution
// back to flagless, stays safe when run twice, and leaves pre-existing
// flagless rows under the empty-flag bucket.
func TestRemoveEventFlagDownBestEffort(t *testing.T) {
	// Given: flagless schema with one flagless daily row + one flagless event.
	app := flaglessFixture(t)
	projID := saveProject(t, app)
	envID := saveEnv(t, app, projID)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dailyID := seedDaily(t, app, dailySeed{
		id: "rst000000000001", day: day, env: envID,
		variant: "control", version: 1, fetches: 2, exposures: 1,
		created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC),
	})
	eventsCol, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	evt := core.NewRecord(eventsCol)
	evt.Set("env", envID)
	evt.Set("ts", time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	if err := app.Save(evt); err != nil {
		t.Fatalf("save event: %v", err)
	}
	if err := restoreEventFlagUp(app, "events"); err != nil {
		t.Fatalf("setup restoreEventFlagUp events: %v", err)
	}
	if err := restoreEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("setup restoreEventFlagUp event_daily: %v", err)
	}

	// When: down runs.
	if err := removeEventFlagDown(app, "events"); err != nil {
		t.Fatalf("removeEventFlagDown events: %v", err)
	}
	if err := removeEventFlagDown(app, "event_daily"); err != nil {
		t.Fatalf("removeEventFlagDown event_daily: %v", err)
	}

	// Then: flag fields are gone from both collections.
	if mustCollection(t, app, "events").Fields.GetByName("flag") != nil {
		t.Errorf("events: flag field still present after down")
	}
	if mustCollection(t, app, "event_daily").Fields.GetByName("flag") != nil {
		t.Errorf("event_daily: flag field still present after down")
	}

	// Then: the env/flag/ts index is gone, upsert key is flagless UNIQUE.
	if got := mustCollection(t, app, "events").GetIndex("idx_events_env_flag_ts"); got != "" {
		t.Errorf("events: idx_events_env_flag_ts still present after down: %q", got)
	}
	upsert := mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after down")
	}
	if !strings.Contains(strings.ToUpper(upsert), "UNIQUE") {
		t.Errorf("event_daily: upsert key not UNIQUE after down: %q", upsert)
	}
	requireUpsertColumns(t, upsert, []string{"day", "env", "variant", "version"})

	// When: down runs again.
	if err := removeEventFlagDown(app, "events"); err != nil {
		t.Fatalf("second removeEventFlagDown events: %v", err)
	}
	if err := removeEventFlagDown(app, "event_daily"); err != nil {
		t.Fatalf("second removeEventFlagDown event_daily: %v", err)
	}

	// Then: double-down is safe and stable.
	if mustCollection(t, app, "events").Fields.GetByName("flag") != nil {
		t.Errorf("events: flag field reappeared after second down")
	}
	upsert = mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after second down")
	}
	requireUpsertColumns(t, upsert, []string{"day", "env", "variant", "version"})

	// Then: pre-existing rows stay under the empty-flag bucket.
	byID := dailyByID(t, app)
	survivor, ok := byID[dailyID]
	if !ok {
		t.Fatalf("seeded daily row %s missing after up+down", dailyID)
	}
	if survivor.GetInt("fetches") != 2 || survivor.GetInt("exposures") != 1 {
		t.Errorf("daily counters = fetches=%d exposures=%d, want 2/1",
			survivor.GetInt("fetches"), survivor.GetInt("exposures"))
	}
	events, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatalf("FindAllRecords events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events has %d rows, want 1", len(events))
	}
}
