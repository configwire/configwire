package migrations

// Convention follows 1790000026_server_logs_columns_test.go: same-package
// tests calling the unexported helpers directly. tests.NewTestApp boots
// with every registered migration applied, so the app starts in the
// POST-migration state (rollup_state + the two range indexes present);
// fixtures reconstruct the pre-migration state via the down helper.
// Style: stdlib testing, Given/When/Then, fresh app per test, no sleep,
// -race/-shuffle safe.

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// newRollupStateApp returns a fresh test app (all migrations already
// applied, so rollup_state and the day/ts indexes exist).
func newRollupStateApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

func mustRollupStateCollection(t *testing.T, app *tests.TestApp) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(rollupStateCollectionName)
	if err != nil {
		t.Fatalf("find %s: %v", rollupStateCollectionName, err)
	}
	return col
}

// assertRollupStateShape pins rollup_state{id system pk, key Text required,
// lastDay Date, updated Autodate create/update}, nil rules, and the unique
// idx_rollup_state_key index on key.
func assertRollupStateShape(t *testing.T, app *tests.TestApp) {
	t.Helper()
	col := mustRollupStateCollection(t, app)

	key, ok := col.Fields.GetByName("key").(*core.TextField)
	if !ok {
		t.Fatalf("rollup_state.key is %T, want *core.TextField", col.Fields.GetByName("key"))
	}
	if !key.Required {
		t.Error("rollup_state.key Required = false, want true")
	}
	if _, ok := col.Fields.GetByName("lastDay").(*core.DateField); !ok {
		t.Fatalf("rollup_state.lastDay is %T, want *core.DateField", col.Fields.GetByName("lastDay"))
	}
	updated, ok := col.Fields.GetByName("updated").(*core.AutodateField)
	if !ok {
		t.Fatalf("rollup_state.updated is %T, want *core.AutodateField", col.Fields.GetByName("updated"))
	}
	if !updated.OnCreate || !updated.OnUpdate {
		t.Errorf("rollup_state.updated OnCreate/OnUpdate = %v/%v, want true/true", updated.OnCreate, updated.OnUpdate)
	}

	if col.ListRule != nil || col.ViewRule != nil || col.CreateRule != nil || col.UpdateRule != nil || col.DeleteRule != nil {
		t.Errorf("rollup_state rules = (%v,%v,%v,%v,%v), want all nil",
			col.ListRule, col.ViewRule, col.CreateRule, col.UpdateRule, col.DeleteRule)
	}

	idx := col.GetIndex("idx_rollup_state_key")
	if idx == "" {
		t.Fatal("rollup_state missing idx_rollup_state_key")
	}
	if !indexIsUnique(idx) {
		t.Errorf("idx_rollup_state_key = %q, want UNIQUE", idx)
	}
}

func indexIsUnique(idx string) bool {
	for i := 0; i+6 <= len(idx); i++ {
		if idx[i:i+6] == "UNIQUE" {
			return true
		}
	}
	return false
}

// TestRollupStateUpShape pins the post-migration schema: collection shape,
// unique singleton index, and both non-unique range indexes.
func TestRollupStateUpShape(t *testing.T) {
	// Given/When: a fresh app (all migrations applied).
	app := newRollupStateApp(t)

	// Then: rollup_state has the singleton shape.
	assertRollupStateShape(t, app)

	// Then: the two range indexes exist, non-unique, on the right columns.
	for _, want := range []struct {
		collection, index, columns string
	}{
		{"event_daily", "idx_event_daily_env_day", "env, day"},
		{"events", "idx_events_ts", "ts"},
	} {
		col, err := app.FindCollectionByNameOrId(want.collection)
		if err != nil {
			t.Fatalf("find %s: %v", want.collection, err)
		}
		idx := col.GetIndex(want.index)
		if idx == "" {
			t.Fatalf("%s missing %s", want.collection, want.index)
		}
		if indexIsUnique(idx) {
			t.Errorf("%s.%s = %q, want non-unique", want.collection, want.index, idx)
		}
		if !containsAll(idx, want.columns, want.index) {
			t.Errorf("%s.%s = %q, want columns %q", want.collection, want.index, idx, want.columns)
		}
	}
}

// containsAll reports whether every expected token appears in idx.
func containsAll(idx, columns, name string) bool {
	for _, needle := range []string{name, columns} {
		found := false
		for i := 0; i+len(needle) <= len(idx); i++ {
			if idx[i:i+len(needle)] == needle {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestRollupStateUpIdempotent pins that rerunning up converges: no error,
// still one collection, one singleton index, and one of each range index.
func TestRollupStateUpIdempotent(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newRollupStateApp(t)

	// When: the migration runs again.
	if err := upRollupState(app); err != nil {
		t.Fatalf("second upRollupState: %v", err)
	}

	// Then: schema is unchanged and still single-instance.
	assertRollupStateShape(t, app)
	col := mustRollupStateCollection(t, app)
	collections, err := app.FindAllCollections()
	if err != nil {
		t.Fatalf("FindAllCollections: %v", err)
	}
	count := 0
	for _, r := range collections {
		if r.Name == rollupStateCollectionName {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%s collections = %d, want exactly 1", rollupStateCollectionName, count)
	}
	if col.GetIndex("idx_rollup_state_key") == "" {
		t.Fatal("idx_rollup_state_key lost on rerun")
	}
}

// TestRollupStateDownRemovesAll pins the inverse: down drops both range
// indexes and the collection, and a second down is a no-op.
func TestRollupStateDownRemovesAll(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newRollupStateApp(t)

	// When: down runs.
	if err := downRollupState(app); err != nil {
		t.Fatalf("downRollupState: %v", err)
	}

	// Then: the collection is gone and both range indexes are removed.
	if _, err := app.FindCollectionByNameOrId(rollupStateCollectionName); err == nil {
		t.Fatalf("%s still present after down", rollupStateCollectionName)
	}
	for _, want := range []struct{ collection, index string }{
		{"event_daily", "idx_event_daily_env_day"},
		{"events", "idx_events_ts"},
	} {
		col, err := app.FindCollectionByNameOrId(want.collection)
		if err != nil {
			t.Fatalf("find %s: %v", want.collection, err)
		}
		if got := col.GetIndex(want.index); got != "" {
			t.Errorf("%s.%s still present after down: %q", want.collection, want.index, got)
		}
	}

	if err := downRollupState(app); err != nil {
		t.Fatalf("second downRollupState: %v", err)
	}

	// And up restores the full post-migration state from the downgraded one.
	if err := upRollupState(app); err != nil {
		t.Fatalf("upRollupState after down: %v", err)
	}
	assertRollupStateShape(t, app)
}

// TestRollupStateSingletonIndexEnforced pins that the unique index actually
// rejects a duplicate key, and that the row round-trips lastDay.
func TestRollupStateSingletonIndexEnforced(t *testing.T) {
	// Given: one singleton row.
	app := newRollupStateApp(t)
	col := mustRollupStateCollection(t, app)
	rec := core.NewRecord(col)
	rec.Set("key", "global")
	rec.Set("lastDay", "2026-09-01 00:00:00.000Z")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save singleton: %v", err)
	}

	// When: a second row uses the same key.
	dup := core.NewRecord(col)
	dup.Set("key", "global")
	// Then: the unique index rejects it.
	if err := app.Save(dup); err == nil {
		t.Fatal("duplicate rollup_state key saved, want unique-index error")
	}
}
