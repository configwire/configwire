package migrations

// Convention follows 1790000025_log_settings_success_test.go:
// same-package tests calling the unexported helpers directly.
// tests.NewTestApp boots with every registered migration applied, so the
// app starts in the POST-migration state (admin-log area fields present);
// the fixture reconstructs the pre-migration state via the down helper
// plus a legacy singleton row. Style: stdlib testing, Given/When/Then,
// fresh app per test, no sleep, -race/-shuffle safe.

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// newLogAdminRequestsApp returns a fresh test app (all migrations
// already applied, so the log_settings admin-log area fields exist).
func newLogAdminRequestsApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// globalAdminLogRow reads the singleton row's admin-log area values.
func globalAdminLogRow(t *testing.T, app *tests.TestApp) (enabled, successLines, healthChecks bool) {
	t.Helper()
	rec, err := app.FindFirstRecordByData("log_settings", "key", "global")
	if err != nil {
		t.Fatalf("find global row: %v", err)
	}
	return rec.GetBool("adminLogsEnabled"), rec.GetBool("adminLogsSuccessLines"), rec.GetBool("adminLogsHealthChecks")
}

// TestLogSettingsAdminRequestsUpAddsFieldAndBackfills pins that up adds
// the three Bool fields and backfills a legacy singleton row to the
// opt-in defaults (off, success lines on, health checks off).
func TestLogSettingsAdminRequestsUpAddsFieldAndBackfills(t *testing.T) {
	// Given: a fresh app reset to the pre-migration state plus a legacy row.
	app := newLogAdminRequestsApp(t)
	if err := downLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("fixture downLogSettingsAdminRequests: %v", err)
	}
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		if f := mustLogSettings(t, app).Fields.GetByName(name); f != nil {
			t.Fatalf("%s present after down, want absent", name)
		}
	}
	seedLegacyGlobalRow(t, app)

	// When: the migration runs.
	if err := upLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("upLogSettingsAdminRequests: %v", err)
	}

	// Then: the fields exist as Bools and the legacy row reads the defaults.
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		f := mustLogSettings(t, app).Fields.GetByName(name)
		if f == nil {
			t.Fatalf("log_settings.%s missing, want present", name)
		} else if _, ok := f.(*core.BoolField); !ok {
			t.Fatalf("log_settings.%s is %T, want *core.BoolField", name, f)
		}
	}
	if enabled, successLines, healthChecks := globalAdminLogRow(t, app); enabled || !successLines || healthChecks {
		t.Errorf("global admin-log area = (%v,%v,%v), want (false,true,false)", enabled, successLines, healthChecks)
	}
}

// TestLogSettingsAdminRequestsUpIdempotent pins that rerunning up
// converges: no error, still exactly one of each field.
func TestLogSettingsAdminRequestsUpIdempotent(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newLogAdminRequestsApp(t)

	// When: up runs again, twice.
	if err := upLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("rerun upLogSettingsAdminRequests: %v", err)
	}
	if err := upLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("second rerun upLogSettingsAdminRequests: %v", err)
	}

	// Then: exactly one of each admin-log field.
	counts := map[string]int{}
	for _, f := range mustLogSettings(t, app).Fields {
		counts[f.GetName()]++
	}
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		if counts[name] != 1 {
			t.Fatalf("%s field count = %d, want exactly 1 (no duplicates)", name, counts[name])
		}
	}
}

// TestLogSettingsAdminRequestsUpWithoutRow pins that up on a collection
// with no singleton row (fresh install) succeeds and seeds nothing —
// EnsureLoaded owns seeding.
func TestLogSettingsAdminRequestsUpWithoutRow(t *testing.T) {
	// Given: a fresh app with the field reverted (no global row exists).
	app := newLogAdminRequestsApp(t)
	if err := downLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("fixture downLogSettingsAdminRequests: %v", err)
	}

	// When: up runs.
	if err := upLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("upLogSettingsAdminRequests: %v", err)
	}

	// Then: the fields exist and no row was fabricated.
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		if f := mustLogSettings(t, app).Fields.GetByName(name); f == nil {
			t.Fatalf("log_settings.%s missing, want present", name)
		}
	}
	if _, err := app.FindFirstRecordByData("log_settings", "key", "global"); err == nil {
		t.Fatal("global row fabricated by migration, want EnsureLoaded to own seeding")
	}
}

// TestLogSettingsAdminRequestsDownRemovesField pins that down drops the
// field and is idempotent, without touching sibling fields or rows.
func TestLogSettingsAdminRequestsDownRemovesField(t *testing.T) {
	// Given: a fresh app (post-migration state) plus a global row.
	app := newLogAdminRequestsApp(t)
	seedLegacyGlobalRow(t, app)
	if err := upLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("fixture upLogSettingsAdminRequests: %v", err)
	}

	// When: down runs.
	if err := downLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("downLogSettingsAdminRequests: %v", err)
	}

	// Then: the fields are gone, siblings and the row survive.
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		if f := mustLogSettings(t, app).Fields.GetByName(name); f != nil {
			t.Errorf("log_settings.%s still present, want dropped", name)
		}
	}
	for _, name := range []string{"key", "enabled", "maxDays", "successLogs"} {
		if f := mustLogSettings(t, app).Fields.GetByName(name); f == nil {
			t.Errorf("log_settings.%s dropped, want surviving", name)
		}
	}
	if _, err := app.FindFirstRecordByData("log_settings", "key", "global"); err != nil {
		t.Fatalf("global row dropped, want surviving: %v", err)
	}

	// And: a rerun converges (idempotent, no error).
	if err := downLogSettingsAdminRequests(app); err != nil {
		t.Fatalf("rerun downLogSettingsAdminRequests: %v", err)
	}
}
