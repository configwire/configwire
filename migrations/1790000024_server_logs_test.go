package migrations

// Convention follows 1790000023_rename_anonid_bucket_test.go:
// same-package tests calling the unexported helpers in
// 1790000024_server_logs.go directly. tests.NewTestApp boots with every
// registered migration applied, so the app starts in the POST-migration
// state (server_logs + log_settings present); the fixture reconstructs
// the pre-migration state via the down helper. Style: stdlib testing,
// Given/When/Then, fresh app per test, no sleep, -race/-shuffle safe.

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newServerLogsApp returns a fresh test app (all migrations already
// applied, so server_logs + log_settings exist).
func newServerLogsApp(t *testing.T) *tests.TestApp {
	t.Helper()

	// Given: a fresh test app (all migrations already applied).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// mustServerLogsCollection finds a collection or fails the test.
func mustServerLogsCollection(t *testing.T, app *tests.TestApp, name string) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		t.Fatalf("find %s: %v", name, err)
	}
	return col
}

// assertNilDenyRules pins the nil-deny posture: every API rule is nil
// (deny non-superusers; "" would be public).
func assertNilDenyRules(t *testing.T, col *core.Collection) {
	t.Helper()
	if col.ListRule != nil {
		t.Errorf("%s: ListRule = %q, want nil (deny)", col.Name, *col.ListRule)
	}
	if col.ViewRule != nil {
		t.Errorf("%s: ViewRule = %q, want nil (deny)", col.Name, *col.ViewRule)
	}
	if col.CreateRule != nil {
		t.Errorf("%s: CreateRule = %q, want nil (deny)", col.Name, *col.CreateRule)
	}
	if col.UpdateRule != nil {
		t.Errorf("%s: UpdateRule = %q, want nil (deny)", col.Name, *col.UpdateRule)
	}
	if col.DeleteRule != nil {
		t.Errorf("%s: DeleteRule = %q, want nil (deny)", col.Name, *col.DeleteRule)
	}
}

// assertServerLogsShape pins server_logs{level Number OnlyInt,
// message Text required presentable, src Text} plus the created index.
func assertServerLogsShape(t *testing.T, app *tests.TestApp) {
	t.Helper()
	col := mustServerLogsCollection(t, app, "server_logs")

	level := col.Fields.GetByName("level")
	if level == nil {
		t.Fatal("server_logs.level missing, want present")
	}
	if nf, ok := level.(*core.NumberField); !ok {
		t.Fatalf("server_logs.level is %T, want *core.NumberField", level)
	} else if !nf.OnlyInt {
		t.Error("server_logs.level OnlyInt = false, want true")
	}

	message := col.Fields.GetByName("message")
	if message == nil {
		t.Fatal("server_logs.message missing, want present")
	}
	if tf, ok := message.(*core.TextField); !ok {
		t.Fatalf("server_logs.message is %T, want *core.TextField", message)
	} else {
		if !tf.Required {
			t.Error("server_logs.message Required = false, want true")
		}
		if !tf.Presentable {
			t.Error("server_logs.message Presentable = false, want true")
		}
	}

	if src := col.Fields.GetByName("src"); src == nil {
		t.Fatal("server_logs.src missing, want present")
	} else if _, ok := src.(*core.TextField); !ok {
		t.Fatalf("server_logs.src is %T, want *core.TextField", src)
	}

	if created := col.Fields.GetByName("created"); created == nil {
		t.Fatal("server_logs.created missing, want present")
	} else if af, ok := created.(*core.AutodateField); !ok {
		t.Fatalf("server_logs.created is %T, want *core.AutodateField", created)
	} else if !af.OnCreate {
		t.Error("server_logs.created OnCreate = false, want true")
	}

	if updated := col.Fields.GetByName("updated"); updated == nil {
		t.Fatal("server_logs.updated missing, want present")
	} else if _, ok := updated.(*core.AutodateField); !ok {
		t.Fatalf("server_logs.updated is %T, want *core.AutodateField", updated)
	}

	assertNilDenyRules(t, col)

	if sql := col.GetIndex("idx_server_logs_created"); sql == "" {
		t.Error("server_logs: index idx_server_logs_created missing, want present")
	} else if !strings.Contains(sql, "created") {
		t.Errorf("server_logs: index idx_server_logs_created = %q, want it on created", sql)
	}
}

// assertLogSettingsShape pins log_settings{key Text required,
// enabled Bool, maxDays Number OnlyInt} plus the UNIQUE key index.
func assertLogSettingsShape(t *testing.T, app *tests.TestApp) {
	t.Helper()
	col := mustServerLogsCollection(t, app, "log_settings")

	key := col.Fields.GetByName("key")
	if key == nil {
		t.Fatal("log_settings.key missing, want present")
	}
	if tf, ok := key.(*core.TextField); !ok {
		t.Fatalf("log_settings.key is %T, want *core.TextField", key)
	} else if !tf.Required {
		t.Error("log_settings.key Required = false, want true")
	}

	if enabled := col.Fields.GetByName("enabled"); enabled == nil {
		t.Fatal("log_settings.enabled missing, want present")
	} else if _, ok := enabled.(*core.BoolField); !ok {
		t.Fatalf("log_settings.enabled is %T, want *core.BoolField", enabled)
	}

	maxDays := col.Fields.GetByName("maxDays")
	if maxDays == nil {
		t.Fatal("log_settings.maxDays missing, want present")
	}
	if nf, ok := maxDays.(*core.NumberField); !ok {
		t.Fatalf("log_settings.maxDays is %T, want *core.NumberField", maxDays)
	} else if !nf.OnlyInt {
		t.Error("log_settings.maxDays OnlyInt = false, want true")
	}

	assertNilDenyRules(t, col)

	if sql := col.GetIndex("idx_log_settings_key"); sql == "" {
		t.Error("log_settings: index idx_log_settings_key missing, want present")
	} else if !strings.Contains(strings.ToUpper(sql), "UNIQUE") {
		t.Errorf("log_settings: index idx_log_settings_key is not UNIQUE: %s", sql)
	}
}

// countCollections counts collections carrying name.
func countCollections(t *testing.T, app *tests.TestApp, name string) int {
	t.Helper()
	all, err := app.FindAllCollections()
	if err != nil {
		t.Fatalf("FindAllCollections: %v", err)
	}
	n := 0
	for _, col := range all {
		if col.Name == name {
			n++
		}
	}
	return n
}

// TestServerLogsUpCreatesCollections pins that up (re)creates both
// collections with the expected shape.
func TestServerLogsUpCreatesCollections(t *testing.T) {
	// Given: a fresh app reset to the pre-migration state.
	app := newServerLogsApp(t)
	if err := downServerLogs(app); err != nil {
		t.Fatalf("fixture downServerLogs: %v", err)
	}

	// When: the migration runs.
	if err := upServerLogs(app); err != nil {
		t.Fatalf("upServerLogs: %v", err)
	}

	// Then: both collections exist with the expected shape.
	assertServerLogsShape(t, app)
	assertLogSettingsShape(t, app)
}

// TestServerLogsUpIdempotent pins that rerunning up converges: no error
// and no duplicate collections.
func TestServerLogsUpIdempotent(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newServerLogsApp(t)

	// When: up runs again.
	if err := upServerLogs(app); err != nil {
		t.Fatalf("rerun upServerLogs: %v", err)
	}

	// Then: still exactly one of each, with the expected shape.
	if n := countCollections(t, app, "server_logs"); n != 1 {
		t.Fatalf("server_logs count = %d, want exactly 1 (no duplicates)", n)
	}
	if n := countCollections(t, app, "log_settings"); n != 1 {
		t.Fatalf("log_settings count = %d, want exactly 1 (no duplicates)", n)
	}
	assertServerLogsShape(t, app)
	assertLogSettingsShape(t, app)

	// And: a second rerun still converges.
	if err := upServerLogs(app); err != nil {
		t.Fatalf("second rerun upServerLogs: %v", err)
	}
}

// TestServerLogsDownDropsCollections pins that down drops both
// collections and is idempotent.
func TestServerLogsDownDropsCollections(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newServerLogsApp(t)

	// When: down runs.
	if err := downServerLogs(app); err != nil {
		t.Fatalf("downServerLogs: %v", err)
	}

	// Then: both collections are gone.
	if _, err := app.FindCollectionByNameOrId("server_logs"); err == nil {
		t.Fatal("server_logs still present, want dropped")
	}
	if _, err := app.FindCollectionByNameOrId("log_settings"); err == nil {
		t.Fatal("log_settings still present, want dropped")
	}

	// And: a rerun converges (idempotent, no error).
	if err := downServerLogs(app); err != nil {
		t.Fatalf("rerun downServerLogs: %v", err)
	}
}

// TestServerLogsDownLeavesOthers pins that down drops ONLY the two
// log collections: a canary collection and the core events table survive.
func TestServerLogsDownLeavesOthers(t *testing.T) {
	// Given: a fresh app plus a canary collection.
	app := newServerLogsApp(t)
	canary := core.NewBaseCollection("cw_server_logs_canary", "cw_server_logs_canary")
	canary.Fields.Add(&core.TextField{Name: "note"})
	if err := app.Save(canary); err != nil {
		t.Fatalf("save canary collection: %v", err)
	}

	// When: down runs.
	if err := downServerLogs(app); err != nil {
		t.Fatalf("downServerLogs: %v", err)
	}

	// Then: the canary survives untouched.
	if _, err := app.FindCollectionByNameOrId("cw_server_logs_canary"); err != nil {
		t.Fatalf("canary collection dropped, want surviving: %v", err)
	}

	// And: unrelated core collections survive too.
	if _, err := app.FindCollectionByNameOrId("events"); err != nil {
		t.Fatalf("events collection touched, want surviving: %v", err)
	}

	// And: up still restores both log collections afterwards.
	if err := upServerLogs(app); err != nil {
		t.Fatalf("upServerLogs after down: %v", err)
	}
	assertServerLogsShape(t, app)
	assertLogSettingsShape(t, app)
}
