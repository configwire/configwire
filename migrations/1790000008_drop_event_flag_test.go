package migrations

// Convention setter: first migrations/*_test.go in the repo.
//
// Same-package tests calling the unexported helpers in
// 1790000008_drop_event_flag.go directly (dropEventFlagUp,
// addEventFlagDown, foldEventDaily). tests.NewTestApp boots with every
// registered migration applied, so the app starts in the POST-drop state
// (no flag fields, flagless upsert key); the newOldSchemaApp fixture
// reconstructs the pre-drop state via addEventFlagDown plus seed rows
// (project, env slug "e1", flags fA/fB). env stays the real required
// relation (not text): the drop migration only touches flag storage,
// and rebuilding collections from scratch collides with the migrated
// TestApp. Style follows the repo: stdlib testing, Given/When/Then,
// tests.NewTestApp + cleanup, no sleep, -race/-shuffle safe (fresh app
// per test, no shared state).
//
// Note: references/go/{README,testing}.md do not exist in this checkout,
// so the repo's own test style (stats/stats_handler_test.go,
// purge/parity_test.go) is the authority. testify is unused anywhere in
// the repo, so plain stdlib assertions keep the convention.

import (
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newOldSchemaApp reconstructs the pre-drop schema on top of the fully
// migrated TestApp: flag relation + old indexes on events/event_daily,
// one project, one env (slug "e1") and two flags (keys fA/fB). It
// returns the app with the env, flagA and flagB record ids for seeds.
func newOldSchemaApp(t *testing.T) (*tests.TestApp, string, string, string) {
	t.Helper()

	// Given: a fresh test app (all migrations already applied).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	// Re-add the pre-drop flag storage the migration under test removes.
	if err := addEventFlagDown(app, "events"); err != nil {
		t.Fatalf("fixture addEventFlagDown events: %v", err)
	}
	if err := addEventFlagDown(app, "event_daily"); err != nil {
		t.Fatalf("fixture addEventFlagDown event_daily: %v", err)
	}

	projID := saveProject(t, app)
	envID := saveEnv(t, app, projID)
	return app, envID, saveFlag(t, app, projID, "fA"), saveFlag(t, app, projID, "fB")
}

func saveProject(t *testing.T, app *tests.TestApp) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		t.Fatalf("find projects: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "dropflag-proj")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save project: %v", err)
	}
	return rec.Id
}

func saveEnv(t *testing.T, app *tests.TestApp, projID string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", projID)
	rec.Set("slug", "e1")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env: %v", err)
	}
	return rec.Id
}

func saveFlag(t *testing.T, app *tests.TestApp, projID, key string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", key)
	rec.Set("type", "bool")
	rec.Set("project", projID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save flag %s: %v", key, err)
	}
	return rec.Id
}

// dailySeed is one event_daily row. Empty flagID leaves the nullable
// flag unset; zero created leaves the autodate default in place.
type dailySeed struct {
	id        string
	day       time.Time
	env       string
	flagID    string
	variant   string
	version   int
	fetches   int
	exposures int
	created   time.Time
}

func seedDaily(t *testing.T, app *tests.TestApp, s dailySeed) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		t.Fatalf("find event_daily: %v", err)
	}
	rec := core.NewRecord(col)
	if s.id != "" {
		rec.Id = s.id
	}
	rec.Set("day", s.day)
	rec.Set("env", s.env)
	if s.flagID != "" {
		rec.Set("flag", s.flagID)
	}
	rec.Set("variant", s.variant)
	rec.Set("version", s.version)
	rec.Set("fetches", s.fetches)
	rec.Set("exposures", s.exposures)
	if !s.created.IsZero() {
		// AutodateField ignores Set (noop setter) but honors a
		// manually SetRaw value on create, giving deterministic
		// created ordering for the fold tests.
		rec.SetRaw("created", s.created)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save event_daily %+v: %v", s, err)
	}
	return rec.Id
}

func seedEventWithFlag(t *testing.T, app *tests.TestApp, envID, flagID string, ts time.Time) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", envID)
	rec.Set("flag", flagID)
	rec.Set("ts", ts)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save event: %v", err)
	}
	return rec.Id
}

func mustCollection(t *testing.T, app *tests.TestApp, name string) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		t.Fatalf("find collection %s: %v", name, err)
	}
	return col
}

func dailyByID(t *testing.T, app *tests.TestApp) map[string]*core.Record {
	t.Helper()
	recs, err := app.FindAllRecords("event_daily")
	if err != nil {
		t.Fatalf("FindAllRecords event_daily: %v", err)
	}
	byID := make(map[string]*core.Record, len(recs))
	for _, r := range recs {
		byID[r.Id] = r
	}
	return byID
}

// requireFlagRelation pins the down-migration contract: flag comes back
// as a nullable single cascade-delete relation to cw_flags.
func requireFlagRelation(t *testing.T, app *tests.TestApp, collection string) {
	t.Helper()
	field := mustCollection(t, app, collection).Fields.GetByName("flag")
	if field == nil {
		t.Fatalf("%s: flag field missing after down", collection)
	}
	rel, ok := field.(*core.RelationField)
	if !ok {
		t.Fatalf("%s: flag field is %T, want *core.RelationField", collection, field)
	}
	if rel.CollectionId != colFlags {
		t.Errorf("%s: flag.CollectionId = %q, want %q", collection, rel.CollectionId, colFlags)
	}
	if rel.MaxSelect != 1 {
		t.Errorf("%s: flag.MaxSelect = %d, want 1", collection, rel.MaxSelect)
	}
	if rel.Required {
		t.Errorf("%s: flag.Required = true, want false (nullable)", collection)
	}
	if !rel.CascadeDelete {
		t.Errorf("%s: flag.CascadeDelete = false, want true", collection)
	}
}

// TestDropEventFlagUp proves up removes the flag attribution storage:
// flag fields gone from both collections, idx_events_env_flag_ts
// deleted while idx_events_env_ts stays, and the daily upsert key
// rekeyed flagless as UNIQUE (day, env, variant, version).
func TestDropEventFlagUp(t *testing.T) {
	// Given: the pre-drop schema with flag attribution.
	app, _, _, _ := newOldSchemaApp(t)

	// When: the up migration runs on both collections.
	if err := dropEventFlagUp(app, "events"); err != nil {
		t.Fatalf("dropEventFlagUp events: %v", err)
	}
	if err := dropEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("dropEventFlagUp event_daily: %v", err)
	}

	// Then: events loses flag + its index, keeps the env/ts index.
	events := mustCollection(t, app, "events")
	if events.Fields.GetByName("flag") != nil {
		t.Errorf("events: flag field still present after up")
	}
	if got := events.GetIndex("idx_events_env_flag_ts"); got != "" {
		t.Errorf("events: idx_events_env_flag_ts still present after up: %q", got)
	}
	if got := events.GetIndex("idx_events_env_ts"); got == "" {
		t.Errorf("events: idx_events_env_ts missing after up (must stay)")
	}

	// Then: event_daily loses flag and its upsert key goes flagless.
	daily := mustCollection(t, app, "event_daily")
	if daily.Fields.GetByName("flag") != nil {
		t.Errorf("event_daily: flag field still present after up")
	}
	upsert := daily.GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after up")
	}
	if !strings.Contains(upsert, "UNIQUE") {
		t.Errorf("event_daily: upsert key not UNIQUE after up: %q", upsert)
	}
	if strings.Contains(upsert, "flag") {
		t.Errorf("event_daily: upsert key still references flag after up: %q", upsert)
	}
	for _, col := range []string{"day", "env", "variant", "version"} {
		if !strings.Contains(upsert, col) {
			t.Errorf("event_daily: upsert key missing %q after up: %q", col, upsert)
		}
	}
}

// TestFoldEventDailyMerges proves the pre-drop fold merges rows that
// only differ by flag: counters sum, earliest-created wins, created
// ties break by lowest Id, singletons and distinct variant/version
// rows are untouched.
func TestFoldEventDailyMerges(t *testing.T) {
	// Given: pre-drop schema with a merge pair, a created-tie pair, a
	// singleton and a distinct-variant row (day 2026-09-01, env e1).
	app, envID, flagAID, flagBID := newOldSchemaApp(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	early := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	tieAt := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	seedDaily(t, app, dailySeed{
		id: "mrg000000000001", day: day, env: envID, flagID: flagAID,
		variant: "control", version: 1, fetches: 2, exposures: 1, created: early,
	})
	seedDaily(t, app, dailySeed{
		id: "mrg000000000002", day: day, env: envID, flagID: flagBID,
		variant: "control", version: 1, fetches: 3, exposures: 4, created: late,
	})
	seedDaily(t, app, dailySeed{
		id: "tie000000000001", day: day, env: envID, flagID: flagAID,
		variant: "tie", version: 1, fetches: 1, exposures: 1, created: tieAt,
	})
	seedDaily(t, app, dailySeed{
		id: "tie000000000002", day: day, env: envID, flagID: flagBID,
		variant: "tie", version: 1, fetches: 1, exposures: 1, created: tieAt,
	})
	seedDaily(t, app, dailySeed{
		id: "sgl000000000001", day: day, env: envID,
		variant: "control", version: 2, fetches: 7, exposures: 8, created: early,
	})
	seedDaily(t, app, dailySeed{
		id: "var000000000001", day: day, env: envID, flagID: flagAID,
		variant: "treatment", version: 1, fetches: 5, exposures: 6, created: early,
	})

	// When: the fold runs.
	if err := foldEventDaily(app); err != nil {
		t.Fatalf("foldEventDaily: %v", err)
	}

	// Then: 6 rows collapse to 4 (one survivor per merged pair).
	byID := dailyByID(t, app)
	if len(byID) != 4 {
		t.Fatalf("event_daily has %d rows after fold, want 4", len(byID))
	}

	// Then: merge pair sums to 5/5 on the earliest-created survivor.
	merged, ok := byID["mrg000000000001"]
	if !ok {
		t.Fatalf("earliest-created merge survivor mrg000000000001 missing after fold")
	}
	if merged.GetInt("fetches") != 5 || merged.GetInt("exposures") != 5 {
		t.Errorf("merge survivor = fetches=%d exposures=%d, want 5/5",
			merged.GetInt("fetches"), merged.GetInt("exposures"))
	}
	if merged.GetString("flag") != flagAID {
		t.Errorf("merge survivor flag = %q, want survivor's own %q", merged.GetString("flag"), flagAID)
	}
	if _, ok := byID["mrg000000000002"]; ok {
		t.Errorf("later-created merge row mrg000000000002 still present after fold")
	}

	// Then: created tie breaks by lowest Id with summed counters.
	tied, ok := byID["tie000000000001"]
	if !ok {
		t.Fatalf("tie survivor tie000000000001 missing after fold")
	}
	if tied.GetInt("fetches") != 2 || tied.GetInt("exposures") != 2 {
		t.Errorf("tie survivor = fetches=%d exposures=%d, want 2/2",
			tied.GetInt("fetches"), tied.GetInt("exposures"))
	}
	if _, ok := byID["tie000000000002"]; ok {
		t.Errorf("tie loser tie000000000002 still present after fold")
	}

	// Then: singleton and distinct-variant rows are untouched.
	single, ok := byID["sgl000000000001"]
	if !ok {
		t.Fatalf("singleton sgl000000000001 missing after fold")
	}
	if single.GetInt("fetches") != 7 || single.GetInt("exposures") != 8 {
		t.Errorf("singleton = fetches=%d exposures=%d, want 7/8",
			single.GetInt("fetches"), single.GetInt("exposures"))
	}
	other, ok := byID["var000000000001"]
	if !ok {
		t.Fatalf("distinct-variant row var000000000001 missing after fold")
	}
	if other.GetInt("fetches") != 5 || other.GetInt("exposures") != 6 {
		t.Errorf("distinct-variant row = fetches=%d exposures=%d, want 5/6",
			other.GetInt("fetches"), other.GetInt("exposures"))
	}

	// Then: fold is schema-neutral (the flag field stays for up to drop).
	if mustCollection(t, app, "event_daily").Fields.GetByName("flag") == nil {
		t.Errorf("event_daily: fold removed the flag field (only up may drop it)")
	}
}

// TestDropEventFlagIdempotent proves re-running up is nil-safe and that
// absent collections are skipped instead of erroring.
func TestDropEventFlagIdempotent(t *testing.T) {
	// Given: the pre-drop schema, already migrated once.
	app, _, _, _ := newOldSchemaApp(t)
	if err := dropEventFlagUp(app, "events"); err != nil {
		t.Fatalf("first dropEventFlagUp events: %v", err)
	}
	if err := dropEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("first dropEventFlagUp event_daily: %v", err)
	}

	// When: up runs a second time.
	if err := dropEventFlagUp(app, "events"); err != nil {
		t.Fatalf("second dropEventFlagUp events: %v", err)
	}
	if err := dropEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("second dropEventFlagUp event_daily: %v", err)
	}

	// Then: schema stays in the post-up shape.
	if mustCollection(t, app, "events").Fields.GetByName("flag") != nil {
		t.Errorf("events: flag field reappeared after second up")
	}
	if mustCollection(t, app, "event_daily").Fields.GetByName("flag") != nil {
		t.Errorf("event_daily: flag field reappeared after second up")
	}
	if got := mustCollection(t, app, "events").GetIndex("idx_events_env_flag_ts"); got != "" {
		t.Errorf("events: idx_events_env_flag_ts reappeared after second up: %q", got)
	}
	upsert := mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" || strings.Contains(upsert, "flag") {
		t.Errorf("event_daily: upsert key not stably flagless after second up: %q", upsert)
	}

	// Then: missing collections are nil-safe.
	if err := dropEventFlagUp(app, "no_such_collection"); err != nil {
		t.Errorf("dropEventFlagUp missing collection: %v, want nil", err)
	}
	if err := addEventFlagDown(app, "no_such_collection"); err != nil {
		t.Errorf("addEventFlagDown missing collection: %v, want nil", err)
	}
	daily := mustCollection(t, app, "event_daily")
	if err := app.Delete(daily); err != nil {
		t.Fatalf("delete event_daily: %v", err)
	}
	if err := foldEventDaily(app); err != nil {
		t.Errorf("foldEventDaily without event_daily: %v, want nil", err)
	}
}

// TestAddEventFlagDownBestEffort proves down re-adds the nullable
// cascade flag relation plus the old indexes, stays safe when run
// twice, and cannot restore the dropped flag values.
func TestAddEventFlagDownBestEffort(t *testing.T) {
	// Given: pre-drop schema with one flagged daily row and one flagged event.
	app, envID, flagAID, _ := newOldSchemaApp(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dailyID := seedDaily(t, app, dailySeed{
		id: "dwn000000000001", day: day, env: envID, flagID: flagAID,
		variant: "control", version: 1, fetches: 2, exposures: 1,
		created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC),
	})
	seedEventWithFlag(t, app, envID, flagAID, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	if err := dropEventFlagUp(app, "events"); err != nil {
		t.Fatalf("setup dropEventFlagUp events: %v", err)
	}
	if err := dropEventFlagUp(app, "event_daily"); err != nil {
		t.Fatalf("setup dropEventFlagUp event_daily: %v", err)
	}

	// When: down runs.
	if err := addEventFlagDown(app, "events"); err != nil {
		t.Fatalf("addEventFlagDown events: %v", err)
	}
	if err := addEventFlagDown(app, "event_daily"); err != nil {
		t.Fatalf("addEventFlagDown event_daily: %v", err)
	}

	// Then: flag is back as nullable single cascade-delete relation.
	requireFlagRelation(t, app, "events")
	requireFlagRelation(t, app, "event_daily")

	// Then: old indexes are back.
	if got := mustCollection(t, app, "events").GetIndex("idx_events_env_flag_ts"); got == "" {
		t.Errorf("events: idx_events_env_flag_ts missing after down")
	}
	upsert := mustCollection(t, app, "event_daily").GetIndex("idx_event_daily_upsert_key")
	if upsert == "" {
		t.Fatalf("event_daily: idx_event_daily_upsert_key missing after down")
	}
	if !strings.Contains(upsert, "flag") {
		t.Errorf("event_daily: upsert key missing flag after down: %q", upsert)
	}

	// When: down runs again.
	if err := addEventFlagDown(app, "events"); err != nil {
		t.Fatalf("second addEventFlagDown events: %v", err)
	}
	if err := addEventFlagDown(app, "event_daily"); err != nil {
		t.Fatalf("second addEventFlagDown event_daily: %v", err)
	}

	// Then: double-down is safe and stable.
	requireFlagRelation(t, app, "events")
	requireFlagRelation(t, app, "event_daily")

	// Then: dropped flag values are not restored.
	byID := dailyByID(t, app)
	survivor, ok := byID[dailyID]
	if !ok {
		t.Fatalf("seeded daily row %s missing after up+down", dailyID)
	}
	if got := survivor.GetString("flag"); got != "" {
		t.Errorf("daily flag value = %q after up+down, want empty (values not restored)", got)
	}
	events, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatalf("FindAllRecords events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events has %d rows, want 1", len(events))
	}
	if got := events[0].GetString("flag"); got != "" {
		t.Errorf("event flag value = %q after up+down, want empty (values not restored)", got)
	}
}
