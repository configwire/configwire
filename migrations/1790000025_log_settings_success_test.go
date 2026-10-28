package migrations

// Convention follows 1790000024_server_logs_test.go: same-package
// tests calling the unexported helpers directly. tests.NewTestApp
// boots with every registered migration applied, so the app starts in
// the POST-migration state (successLogs field present); the fixture
// reconstructs the pre-migration state via the down helper plus a
// legacy singleton row. Style: stdlib testing, Given/When/Then, fresh
// app per test, no sleep, -race/-shuffle safe.

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// newLogSuccessApp returns a fresh test app (all migrations already
// applied, so log_settings.successLogs exists).
func newLogSuccessApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// mustLogSettings finds the log_settings collection or fails the test.
func mustLogSettings(t *testing.T, app *tests.TestApp) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("log_settings")
	if err != nil {
		t.Fatalf("find log_settings: %v", err)
	}
	return col
}

// seedLegacyGlobalRow writes the pre-migration singleton row (no
// successLogs field on the collection, so nothing to set).
func seedLegacyGlobalRow(t *testing.T, app *tests.TestApp) {
	t.Helper()
	col := mustLogSettings(t, app)
	rec := core.NewRecord(col)
	rec.Set("key", "global")
	rec.Set("enabled", true)
	rec.Set("maxDays", 30)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed global row: %v", err)
	}
}

// globalSuccessLogs reads the singleton row's successLogs value.
func globalSuccessLogs(t *testing.T, app *tests.TestApp) bool {
	t.Helper()
	rec, err := app.FindFirstRecordByData("log_settings", "key", "global")
	if err != nil {
		t.Fatalf("find global row: %v", err)
	}
	return rec.GetBool("successLogs")
}

// TestLogSettingsSuccessUpAddsFieldAndBackfills pins that up adds the
// Bool field and flips a legacy singleton row to true (false was
// unrepresentable before the field existed).
func TestLogSettingsSuccessUpAddsFieldAndBackfills(t *testing.T) {
	// Given: a fresh app reset to the pre-migration state plus a legacy row.
	app := newLogSuccessApp(t)
	if err := downLogSettingsSuccess(app); err != nil {
		t.Fatalf("fixture downLogSettingsSuccess: %v", err)
	}
	if f := mustLogSettings(t, app).Fields.GetByName("successLogs"); f != nil {
		t.Fatalf("successLogs present after down, want absent")
	}
	seedLegacyGlobalRow(t, app)

	// When: the migration runs.
	if err := upLogSettingsSuccess(app); err != nil {
		t.Fatalf("upLogSettingsSuccess: %v", err)
	}

	// Then: the field exists as a Bool and the legacy row reads true.
	if f := mustLogSettings(t, app).Fields.GetByName("successLogs"); f == nil {
		t.Fatal("log_settings.successLogs missing, want present")
	} else if _, ok := f.(*core.BoolField); !ok {
		t.Fatalf("log_settings.successLogs is %T, want *core.BoolField", f)
	}
	if !globalSuccessLogs(t, app) {
		t.Error("global successLogs = false, want true (backfill)")
	}
}

// TestLogSettingsSuccessUpIdempotent pins that rerunning up converges:
// no error, still exactly one field, row stays true.
func TestLogSettingsSuccessUpIdempotent(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newLogSuccessApp(t)

	// When: up runs again, twice.
	if err := upLogSettingsSuccess(app); err != nil {
		t.Fatalf("rerun upLogSettingsSuccess: %v", err)
	}
	if err := upLogSettingsSuccess(app); err != nil {
		t.Fatalf("second rerun upLogSettingsSuccess: %v", err)
	}

	// Then: exactly one successLogs field.
	n := 0
	for _, f := range mustLogSettings(t, app).Fields {
		if f.GetName() == "successLogs" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("successLogs field count = %d, want exactly 1 (no duplicates)", n)
	}
}

// TestLogSettingsSuccessUpWithoutRow pins that up on a collection with
// no singleton row (fresh install) succeeds and seeds nothing —
// EnsureLoaded owns seeding.
func TestLogSettingsSuccessUpWithoutRow(t *testing.T) {
	// Given: a fresh app with the field reverted (no global row exists).
	app := newLogSuccessApp(t)
	if err := downLogSettingsSuccess(app); err != nil {
		t.Fatalf("fixture downLogSettingsSuccess: %v", err)
	}

	// When: up runs.
	if err := upLogSettingsSuccess(app); err != nil {
		t.Fatalf("upLogSettingsSuccess: %v", err)
	}

	// Then: the field exists and no row was fabricated.
	if f := mustLogSettings(t, app).Fields.GetByName("successLogs"); f == nil {
		t.Fatal("log_settings.successLogs missing, want present")
	}
	if _, err := app.FindFirstRecordByData("log_settings", "key", "global"); err == nil {
		t.Fatal("global row fabricated by migration, want EnsureLoaded to own seeding")
	}
}

// TestLogSettingsSuccessDownRemovesField pins that down drops the field
// and is idempotent, without touching sibling fields or rows.
func TestLogSettingsSuccessDownRemovesField(t *testing.T) {
	// Given: a fresh app (post-migration state) plus a global row.
	app := newLogSuccessApp(t)
	seedLegacyGlobalRow(t, app)
	if err := upLogSettingsSuccess(app); err != nil {
		t.Fatalf("fixture upLogSettingsSuccess: %v", err)
	}

	// When: down runs.
	if err := downLogSettingsSuccess(app); err != nil {
		t.Fatalf("downLogSettingsSuccess: %v", err)
	}

	// Then: the field is gone, siblings and the row survive.
	if f := mustLogSettings(t, app).Fields.GetByName("successLogs"); f != nil {
		t.Fatal("log_settings.successLogs still present, want dropped")
	}
	for _, name := range []string{"key", "enabled", "maxDays"} {
		if f := mustLogSettings(t, app).Fields.GetByName(name); f == nil {
			t.Errorf("log_settings.%s dropped, want surviving", name)
		}
	}
	if _, err := app.FindFirstRecordByData("log_settings", "key", "global"); err != nil {
		t.Fatalf("global row dropped, want surviving: %v", err)
	}

	// And: a rerun converges (idempotent, no error).
	if err := downLogSettingsSuccess(app); err != nil {
		t.Fatalf("rerun downLogSettingsSuccess: %v", err)
	}
}
