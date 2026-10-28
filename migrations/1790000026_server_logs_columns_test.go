package migrations

// Convention follows 1790000025_log_settings_success_test.go: same-package
// tests calling the unexported helpers directly. tests.NewTestApp boots with
// every registered migration applied, so the app starts in the
// POST-migration state (filter columns present); the fixture reconstructs
// the pre-migration state via the down helper plus legacy message-only
// rows. Style: stdlib testing, Given/When/Then, fresh app per test, no
// sleep, -race/-shuffle safe.

import (
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// newServerLogsColumnsApp returns a fresh test app (all migrations already
// applied, so the server_logs filter columns exist).
func newServerLogsColumnsApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// mustServerLogsColumns finds the server_logs collection or fails the test.
func mustServerLogsColumns(t *testing.T, app *tests.TestApp) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("server_logs")
	if err != nil {
		t.Fatalf("find server_logs: %v", err)
	}
	return col
}

// seedLegacyLogRow writes a pre-migration row: level/src/message only (the
// new filter columns do not exist on the collection yet, so nothing to
// set).
func seedLegacyLogRow(t *testing.T, app *tests.TestApp, level int, src, message string) string {
	t.Helper()
	col := mustServerLogsColumns(t, app)
	rec := core.NewRecord(col)
	rec.Set("level", level)
	rec.Set("src", src)
	rec.Set("message", message)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed legacy log row: %v", err)
	}
	return rec.Id
}

// seedProjectRow writes a real projects row and returns its id, so the
// relation backfill and tagged-row seeds have a live target alongside
// dangling tokens.
func seedProjectRow(t *testing.T, app *tests.TestApp, name string) string {
	t.Helper()
	projCol, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		t.Fatalf("find projects: %v", err)
	}
	rec := core.NewRecord(projCol)
	rec.Set("name", name)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed project row: %v", err)
	}
	return rec.Id
}

// serverLogsRowByMessage finds a server_logs row by its exact message.
func serverLogsRowByMessage(t *testing.T, app *tests.TestApp, message string) *core.Record {
	t.Helper()
	rec, err := app.FindFirstRecordByData("server_logs", "message", message)
	if err != nil {
		t.Fatalf("find row %q: %v", message, err)
	}
	return rec
}

// assertServerLogsColumnsShape pins server_logs{project Relation to
// projects (single-select, no cascade), env Relation to environments
// (single-select, no cascade), key_prefix Text, status Number OnlyInt,
// reason Text, limiter Text} plus one non-unique index per column.
func assertServerLogsColumnsShape(t *testing.T, app *tests.TestApp) {
	t.Helper()
	col := mustServerLogsColumns(t, app)
	projCol, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		t.Fatalf("find projects: %v", err)
	}
	envCol, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rel, ok := col.Fields.GetByName("project").(*core.RelationField)
	if !ok {
		t.Fatalf("server_logs.project is %T, want *core.RelationField", col.Fields.GetByName("project"))
	}
	if rel.CollectionId != projCol.Id {
		t.Errorf("server_logs.project.CollectionId = %q, want projects %q", rel.CollectionId, projCol.Id)
	}
	if rel.MaxSelect != 1 {
		t.Errorf("server_logs.project.MaxSelect = %d, want 1", rel.MaxSelect)
	}
	if rel.CascadeDelete {
		t.Error("server_logs.project.CascadeDelete = true, want false (project deletion must never wipe log history)")
	}
	envRel, ok := col.Fields.GetByName("env").(*core.RelationField)
	if !ok {
		t.Fatalf("server_logs.env is %T, want *core.RelationField", col.Fields.GetByName("env"))
	}
	if envRel.CollectionId != envCol.Id {
		t.Errorf("server_logs.env.CollectionId = %q, want environments %q", envRel.CollectionId, envCol.Id)
	}
	if envRel.MaxSelect != 1 {
		t.Errorf("server_logs.env.MaxSelect = %d, want 1", envRel.MaxSelect)
	}
	if envRel.CascadeDelete {
		t.Error("server_logs.env.CascadeDelete = true, want false (env deletion must never wipe log history)")
	}
	for _, name := range []string{"key_prefix", "reason", "limiter"} {
		f := col.Fields.GetByName(name)
		if f == nil {
			t.Fatalf("server_logs.%s missing, want present", name)
		}
		if _, ok := f.(*core.TextField); !ok {
			t.Fatalf("server_logs.%s is %T, want *core.TextField", name, f)
		}
	}
	if f := col.Fields.GetByName("status"); f == nil {
		t.Fatal("server_logs.status missing, want present")
	} else if nf, ok := f.(*core.NumberField); !ok {
		t.Fatalf("server_logs.status is %T, want *core.NumberField", f)
	} else if !nf.OnlyInt {
		t.Error("server_logs.status OnlyInt = false, want true")
	}
	for _, idx := range [][2]string{
		{"idx_server_logs_project", "project"},
		{"idx_server_logs_env", "env"},
		{"idx_server_logs_key_prefix", "key_prefix"},
		{"idx_server_logs_status", "status"},
		{"idx_server_logs_reason", "reason"},
		{"idx_server_logs_limiter", "limiter"},
	} {
		sql := col.GetIndex(idx[0])
		if sql == "" {
			t.Errorf("server_logs: index %s missing, want present", idx[0])
		} else if !strings.Contains(sql, idx[1]) {
			t.Errorf("server_logs: index %s = %q, want it on %s", idx[0], sql, idx[1])
		} else if strings.Contains(strings.ToUpper(sql), "UNIQUE") {
			t.Errorf("server_logs: index %s is UNIQUE, want non-unique: %s", idx[0], sql)
		}
	}
}

// TestServerLogsColumnsUpAddsFieldsIndexesAndBackfills pins that up adds
// the five fields plus indexes and parses the message tokens onto them.
func TestServerLogsColumnsUpAddsFieldsIndexesAndBackfills(t *testing.T) {
	// Given: a fresh app reset to the pre-migration state plus legacy rows.
	app := newServerLogsColumnsApp(t)
	if err := downServerLogsColumns(app); err != nil {
		t.Fatalf("fixture downServerLogsColumns: %v", err)
	}
	for _, name := range serverLogsColumns {
		if f := mustServerLogsColumns(t, app).Fields.GetByName(name); f != nil {
			t.Fatalf("server_logs.%s present after down, want absent", name)
		}
	}
	projectID := seedProjectRow(t, app, "p1-proj")
	full := "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni project=" + projectID
	rejected := "POST /api/v1/env/prod/events reason=rate-limited status=429 key_prefix=cw-OAtni limiter=per-key"
	plain := "GET /api/v1/env/dev/config reason=served status=200"
	badStatus := "GET /api/v1/env/dev/config reason=served status=abc"
	dangling := "GET /api/v1/env/dev/config reason=served status=200 project=gone-proj"
	seedLegacyLogRow(t, app, 1, "ingest", full)
	seedLegacyLogRow(t, app, 2, "ingest", rejected)
	seedLegacyLogRow(t, app, 1, "fetch", plain)
	seedLegacyLogRow(t, app, 1, "fetch", badStatus)
	seedLegacyLogRow(t, app, 1, "fetch", dangling)

	// When: the migration runs.
	if err := upServerLogsColumns(app); err != nil {
		t.Fatalf("upServerLogsColumns: %v", err)
	}

	// Then: fields and indexes exist.
	assertServerLogsColumnsShape(t, app)

	// And: the full row backfilled every token.
	r := serverLogsRowByMessage(t, app, full)
	if got := r.GetString("project"); got != projectID {
		t.Errorf("full row project = %q, want %q", got, projectID)
	}
	if got := r.GetString("key_prefix"); got != "cw-OAtni" {
		t.Errorf("full row key_prefix = %q, want %q", got, "cw-OAtni")
	}
	if got := r.GetInt("status"); got != 202 {
		t.Errorf("full row status = %d, want 202", got)
	}
	if got := r.GetString("reason"); got != "accepted" {
		t.Errorf("full row reason = %q, want %q", got, "accepted")
	}
	if got := r.GetString("limiter"); got != "" {
		t.Errorf("full row limiter = %q, want empty", got)
	}

	// And: the rejection backfilled the limiter and left project empty.
	r = serverLogsRowByMessage(t, app, rejected)
	if got := r.GetInt("status"); got != 429 {
		t.Errorf("rejected row status = %d, want 429", got)
	}
	if got := r.GetString("limiter"); got != "per-key" {
		t.Errorf("rejected row limiter = %q, want %q", got, "per-key")
	}
	if got := r.GetString("project"); got != "" {
		t.Errorf("rejected row project = %q, want empty", got)
	}

	// And: the plain row backfilled status/reason only.
	r = serverLogsRowByMessage(t, app, plain)
	if got := r.GetInt("status"); got != 200 {
		t.Errorf("plain row status = %d, want 200", got)
	}
	if got := r.GetString("reason"); got != "served" {
		t.Errorf("plain row reason = %q, want %q", got, "served")
	}

	// And: the unparseable status was skipped (stays unset).
	r = serverLogsRowByMessage(t, app, badStatus)
	if got := r.GetInt("status"); got != 0 {
		t.Errorf("bad-status row status = %d, want 0 (skipped)", got)
	}
	if got := r.GetString("reason"); got != "served" {
		t.Errorf("bad-status row reason = %q, want %q", got, "served")
	}

	// And: the dangling project token is kept verbatim, never nulled.
	r = serverLogsRowByMessage(t, app, dangling)
	if got := r.GetString("project"); got != "gone-proj" {
		t.Errorf("dangling row project = %q, want %q (kept verbatim)", got, "gone-proj")
	}

	// And: no row gained an env — the backfill never parses env slugs
	// from message paths (paths hold renameable slugs, not env IDs), so
	// pre-migration rows keep env unset.
	for _, msg := range []string{full, rejected, plain, badStatus, dangling} {
		if got := serverLogsRowByMessage(t, app, msg).GetString("env"); got != "" {
			t.Errorf("row %q env = %q, want empty (backfill never parses env)", msg, got)
		}
	}
}

// TestServerLogsColumnsUpIdempotent pins that rerunning up converges: no
// error, still exactly one field and index per column, values stable.
func TestServerLogsColumnsUpIdempotent(t *testing.T) {
	// Given: a fresh app (post-migration state).
	app := newServerLogsColumnsApp(t)

	// When: up runs again, twice.
	if err := upServerLogsColumns(app); err != nil {
		t.Fatalf("rerun upServerLogsColumns: %v", err)
	}
	if err := upServerLogsColumns(app); err != nil {
		t.Fatalf("second rerun upServerLogsColumns: %v", err)
	}

	// Then: exactly one field and one index per column.
	col := mustServerLogsColumns(t, app)
	for _, name := range serverLogsColumns {
		n := 0
		for _, f := range col.Fields {
			if f.GetName() == name {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("server_logs.%s field count = %d, want exactly 1 (no duplicates)", name, n)
		}
	}
	for _, idx := range serverLogsColumnIndexes {
		if sql := col.GetIndex(idx[0]); sql == "" {
			t.Errorf("server_logs: index %s missing after rerun, want present", idx[0])
		}
	}
	assertServerLogsColumnsShape(t, app)
}

// TestServerLogsColumnsDownRemovesFieldsAndIndexesOnly pins that down drops
// the added fields and indexes without touching the collection, sibling
// fields, or rows.
func TestServerLogsColumnsDownRemovesFieldsAndIndexesOnly(t *testing.T) {
	// Given: a fresh app (post-migration state) plus a tagged row.
	app := newServerLogsColumnsApp(t)
	msg := "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni project=p1"
	col := mustServerLogsColumns(t, app)
	rec := core.NewRecord(col)
	rec.Set("level", 1)
	rec.Set("src", "ingest")
	rec.Set("message", msg)
	rec.Set("project", seedProjectRow(t, app, "down-proj"))
	rec.Set("status", 202)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed tagged row: %v", err)
	}

	// When: down runs.
	if err := downServerLogsColumns(app); err != nil {
		t.Fatalf("downServerLogsColumns: %v", err)
	}

	// Then: the added fields and indexes are gone.
	col = mustServerLogsColumns(t, app)
	for _, name := range serverLogsColumns {
		if f := col.Fields.GetByName(name); f != nil {
			t.Errorf("server_logs.%s still present, want dropped", name)
		}
	}
	for _, idx := range serverLogsColumnIndexes {
		if sql := col.GetIndex(idx[0]); sql != "" {
			t.Errorf("server_logs: index %s still present, want dropped: %s", idx[0], sql)
		}
	}

	// And: the collection, sibling fields, the created index, and the row
	// survive.
	if _, err := app.FindCollectionByNameOrId("server_logs"); err != nil {
		t.Fatalf("server_logs collection dropped, want surviving: %v", err)
	}
	for _, name := range []string{"level", "message", "src", "created", "updated"} {
		if f := mustServerLogsColumns(t, app).Fields.GetByName(name); f == nil {
			t.Errorf("server_logs.%s dropped, want surviving", name)
		}
	}
	if sql := mustServerLogsColumns(t, app).GetIndex("idx_server_logs_created"); sql == "" {
		t.Error("server_logs: index idx_server_logs_created dropped, want surviving")
	}
	if _, err := app.FindFirstRecordByData("server_logs", "message", msg); err != nil {
		t.Fatalf("tagged row dropped, want surviving: %v", err)
	}

	// And: a rerun converges (idempotent, no error).
	if err := downServerLogsColumns(app); err != nil {
		t.Fatalf("rerun downServerLogsColumns: %v", err)
	}
}
