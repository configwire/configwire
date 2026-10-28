package logstore

import (
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// logsTestApp builds a TestApp with a server_logs collection mirroring
// the migrated shape (level/message/src plus the project/env/key_prefix/
// status/reason/limiter filter columns and created/updated autodates;
// created drives the newest-first order and purge cutoffs).
func logsTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("server_logs")
	for _, f := range []string{
		`{"type":"number","name":"level","onlyInt":true}`,
		`{"type":"text","name":"message","required":true}`,
		`{"type":"text","name":"src"}`,
		`{"type":"text","name":"project"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"key_prefix"}`,
		`{"type":"number","name":"status","onlyInt":true}`,
		`{"type":"text","name":"reason"}`,
		`{"type":"text","name":"limiter"}`,
		`{"type":"autodate","name":"created","onCreate":true}`,
		`{"type":"autodate","name":"updated","onCreate":true,"onUpdate":true}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save server_logs collection: %v", err)
	}
	return app
}

func seedLog(t *testing.T, app *tests.TestApp, e Entry) {
	t.Helper()
	if err := Write(app, e); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// seedLogRows covers new (project/env-tagged) and old (untagged) rows
// plus a second project and a rejection, so single- and multi-filter
// cases all have signal. The second row carries no env, so env-filter
// cases pin that empty-env rows never match a set filter. Messages keep
// the production k=v line shape; the structured fields ride alongside
// via Entry.
func seedLogRows(t *testing.T, app *tests.TestApp) {
	t.Helper()
	resetForTest(Defaults())
	seedLog(t, app, Entry{Level: 1, Src: "ingest",
		Message: "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni project=p1",
		Project: "p1", Env: "e1", KeyPrefix: "cw-OAtni", Status: 202, Reason: "accepted"})
	seedLog(t, app, Entry{Level: 1, Src: "ingest",
		Message:   "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni",
		KeyPrefix: "cw-OAtni", Status: 202, Reason: "accepted"})
	seedLog(t, app, Entry{Level: 2, Src: "ingest",
		Message: "POST /api/v1/env/prod/events reason=rate-limited status=429 key_prefix=cw-OAtni limiter=per-key",
		Env:     "e1", KeyPrefix: "cw-OAtni", Status: 429, Reason: "rate-limited", Limiter: "per-key"})
	seedLog(t, app, Entry{Level: 1, Src: "fetch",
		Message: "GET /api/v1/env/dev/config reason=served status=200 key_prefix=cw-ZZZZ project=p2",
		Project: "p2", Env: "e2", KeyPrefix: "cw-ZZZZ", Status: 200, Reason: "served"})
}

// countLogs mirrors getLogs: the shared filter expression feeds the total
// count next to Query's page, so the test pins page/total parity.
func countLogs(t *testing.T, app *tests.TestApp, q LogQuery) int64 {
	t.Helper()
	var exprs []dbx.Expression
	if conds, params := q.conditions(); len(conds) > 0 {
		exprs = append(exprs, dbx.NewExp(strings.Join(conds, " AND "), params))
	}
	total, err := app.CountRecords("server_logs", exprs...)
	if err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return total
}

func TestQueryColumnFilters(t *testing.T) {
	app := logsTestApp(t)
	seedLogRows(t, app)

	cases := []struct {
		name  string
		query LogQuery
		want  int
	}{
		{"no filters match all", LogQuery{Level: -1}, 4},
		{"project isolates one row", LogQuery{Level: -1, Project: "p1"}, 1},
		{"project misses other project", LogQuery{Level: -1, Project: "p2"}, 1},
		{"key spans tagged and untagged rows", LogQuery{Level: -1, Key: "cw-OAtni"}, 3},
		{"status matches code", LogQuery{Level: -1, Status: "202"}, 2},
		{"status 429 isolates rejection", LogQuery{Level: -1, Status: "429"}, 1},
		{"status 20 matches nothing, no prefix collision with 200/202", LogQuery{Level: -1, Status: "20"}, 0},
		{"status 200 matches only the exact row", LogQuery{Level: -1, Status: "200"}, 1},
		{"reason plus project narrows to one", LogQuery{Level: -1, Reason: "accepted", Project: "p1"}, 1},
		{"reason plus status narrows to two", LogQuery{Level: -1, Reason: "accepted", Status: "202"}, 2},
		{"rejection columns combine", LogQuery{Level: -1, Reason: "rate-limited", Status: "429", Limiter: "per-key"}, 1},
		{"key plus project combine", LogQuery{Level: -1, Key: "cw-ZZZZ", Project: "p2"}, 1},
		{"limiter isolates rejection", LogQuery{Level: -1, Limiter: "per-key"}, 1},
		{"src still exact-matches", LogQuery{Level: -1, Src: "fetch"}, 1},
		{"level still exact-matches", LogQuery{Level: 2}, 1},
		{"level plus key combine", LogQuery{Level: 1, Key: "cw-OAtni"}, 2},
		{"unknown project matches nothing", LogQuery{Level: -1, Project: "nope"}, 0},
		{"project plus other project matches nothing", LogQuery{Level: -1, Project: "p1", Key: "cw-ZZZZ"}, 0},
		{"env isolates two rows", LogQuery{Level: -1, Env: "e1"}, 2},
		{"env isolates one row", LogQuery{Level: -1, Env: "e2"}, 1},
		{"env plus project narrows to one", LogQuery{Level: -1, Env: "e1", Project: "p1"}, 1},
		{"env plus other project matches nothing", LogQuery{Level: -1, Env: "e1", Project: "p2"}, 0},
		{"env plus status narrows to one", LogQuery{Level: -1, Env: "e1", Status: "429"}, 1},
		{"unknown env matches nothing", LogQuery{Level: -1, Env: "nope"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs, err := Query(app, tc.query, 1, 50)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(recs) != tc.want {
				t.Fatalf("Query = %d rows, want %d", len(recs), tc.want)
			}
			if total := countLogs(t, app, tc.query); total != int64(tc.want) {
				t.Fatalf("CountRecords = %d, want %d (page/total parity)", total, tc.want)
			}
		})
	}
}

func TestQueryColumnPagination(t *testing.T) {
	app := logsTestApp(t)
	seedLogRows(t, app)

	q := LogQuery{Level: -1, Key: "cw-OAtni"} // 3 rows
	first, err := Query(app, q, 1, 2)
	if err != nil {
		t.Fatalf("Query page 1: %v", err)
	}
	second, err := Query(app, q, 2, 2)
	if err != nil {
		t.Fatalf("Query page 2: %v", err)
	}
	if len(first) != 2 || len(second) != 1 {
		t.Fatalf("pages = %d/%d rows, want 2/1", len(first), len(second))
	}
	seen := map[string]bool{}
	for _, r := range append(first, second...) {
		if seen[r.Id] {
			t.Fatalf("duplicate row %s across pages", r.Id)
		}
		seen[r.Id] = true
	}
}

func TestWritePersistsFilterColumns(t *testing.T) {
	app := logsTestApp(t)
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })

	msg := "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni project=p1"
	if err := Write(app, Entry{Level: 1, Src: "ingest", Message: msg,
		Project: "p1", Env: "e1", KeyPrefix: "cw-OAtni", Status: 202, Reason: "accepted"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rec, err := app.FindFirstRecordByData("server_logs", "message", msg)
	if err != nil {
		t.Fatalf("find row: %v", err)
	}
	if got := rec.GetInt("level"); got != 1 {
		t.Errorf("level = %d, want 1", got)
	}
	if got := rec.GetString("src"); got != "ingest" {
		t.Errorf("src = %q, want ingest", got)
	}
	if got := rec.GetString("project"); got != "p1" {
		t.Errorf("project = %q, want p1", got)
	}
	if got := rec.GetString("env"); got != "e1" {
		t.Errorf("env = %q, want e1", got)
	}
	if got := rec.GetString("key_prefix"); got != "cw-OAtni" {
		t.Errorf("key_prefix = %q, want cw-OAtni", got)
	}
	if got := rec.GetInt("status"); got != 202 {
		t.Errorf("status = %d, want 202", got)
	}
	if got := rec.GetString("reason"); got != "accepted" {
		t.Errorf("reason = %q, want accepted", got)
	}
	if got := rec.GetString("limiter"); got != "" {
		t.Errorf("limiter = %q, want empty", got)
	}
}

func TestWriteDefaultsAndValidation(t *testing.T) {
	app := logsTestApp(t)
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })

	if err := Write(app, Entry{Level: 1, Message: "hello"}); err != nil {
		t.Fatalf("Write minimal: %v", err)
	}
	rec, err := app.FindFirstRecordByData("server_logs", "message", "hello")
	if err != nil {
		t.Fatalf("find row: %v", err)
	}
	if got := rec.GetString("src"); got != DefaultSrc {
		t.Errorf("src = %q, want default %q", got, DefaultSrc)
	}
	if got := rec.GetInt("status"); got != 0 {
		t.Errorf("status = %d, want 0 (absent)", got)
	}

	if err := Write(app, Entry{Level: -1, Src: "x", Message: "bad level"}); err == nil {
		t.Error("Write level -1 = nil, want error")
	}
	if err := Write(app, Entry{Level: 4, Src: "x", Message: "bad level"}); err == nil {
		t.Error("Write level 4 = nil, want error")
	}
	if err := Write(app, Entry{Level: 1, Src: "x", Message: "   "}); err == nil {
		t.Error("Write blank message = nil, want error")
	}

	resetForTest(Settings{Enabled: false, MaxDays: 30, SuccessLogs: true})
	if err := Write(app, Entry{Level: 1, Src: "x", Message: "dropped"}); err != nil {
		t.Fatalf("Write disabled = %v, want nil", err)
	}
	if _, err := app.FindFirstRecordByData("server_logs", "message", "dropped"); err == nil {
		t.Error("disabled Write persisted a row, want no-op")
	}
}

func TestValidLogToken(t *testing.T) {
	ok := []string{"p1", "cw-OAtni", "202", "accepted", "per-key", "aB0_-", "_"}
	for _, s := range ok {
		if !validLogToken(s) {
			t.Errorf("validLogToken(%q) = false, want true", s)
		}
	}
	bad := []string{"", "a b", "a%b", "a_b%", "%", "a/b", "a=b", "a;b", "a'b", `a"b`, "a*b", "a+b"}
	for _, s := range bad {
		if validLogToken(s) {
			t.Errorf("validLogToken(%q) = true, want false", s)
		}
	}
}

func TestValidLogStatus(t *testing.T) {
	ok := []string{"0", "20", "200", "202", "429"}
	for _, s := range ok {
		if !validLogStatus(s) {
			t.Errorf("validLogStatus(%q) = false, want true", s)
		}
	}
	bad := []string{"", "20a", "a20", "2 0", "20%", "2.0", "-1", "+200"}
	for _, s := range bad {
		if validLogStatus(s) {
			t.Errorf("validLogStatus(%q) = true, want false", s)
		}
	}
}

// relationLogsTestApp builds a TestApp whose server_logs carries REAL
// RelationFields for project/env (the shared fixture uses text
// stand-ins), pointing at stub collections. This is the only test that
// exercises validated Write plus equality filtering on the relation
// type production runs — without it a relation-specific regression
// (validation, stored-ID shape) would pass the suite unnoticed.
func relationLogsTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	resetForTest(Defaults())

	mkStub := func(name string) *core.Collection {
		col := core.NewBaseCollection(name)
		if err := app.Save(col); err != nil {
			t.Fatalf("save %s collection: %v", name, err)
		}
		return col
	}
	projCol := mkStub("rel_projects")
	envCol := mkStub("rel_envs")

	col := core.NewBaseCollection("server_logs")
	col.Fields.Add(
		&core.NumberField{Name: "level", OnlyInt: true},
		&core.TextField{Name: "message", Required: true},
		&core.TextField{Name: "src"},
		&core.RelationField{Name: "project", CollectionId: projCol.Id, MaxSelect: 1},
		&core.RelationField{Name: "env", CollectionId: envCol.Id, MaxSelect: 1},
		&core.TextField{Name: "key_prefix"},
		&core.NumberField{Name: "status", OnlyInt: true},
		&core.TextField{Name: "reason"},
		&core.TextField{Name: "limiter"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	if err := app.Save(col); err != nil {
		t.Fatalf("save server_logs collection: %v", err)
	}
	return app
}

// TestQueryRelationBackedColumns pins that equality filtering transfers
// to relation-typed columns: validated Write accepts live record IDs,
// Query matches them, dangling IDs are rejected (writers must use live
// records — the migration backfill is the only SaveNoValidate path).
func TestQueryRelationBackedColumns(t *testing.T) {
	app := relationLogsTestApp(t)

	projCol, err := app.FindCollectionByNameOrId("rel_projects")
	if err != nil {
		t.Fatalf("find rel_projects: %v", err)
	}
	proj := core.NewRecord(projCol)
	if err := app.Save(proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	envCol, err := app.FindCollectionByNameOrId("rel_envs")
	if err != nil {
		t.Fatalf("find rel_envs: %v", err)
	}
	env := core.NewRecord(envCol)
	if err := app.Save(env); err != nil {
		t.Fatalf("seed env: %v", err)
	}

	msg := "GET /api/v1/env/rel-env/config reason=served status=200 key_prefix=cw-Rel0 project=" + proj.Id
	if err := Write(app, Entry{Level: 1, Src: "fetch", Message: msg,
		Project: proj.Id, Env: env.Id, KeyPrefix: "cw-Rel0", Status: 200, Reason: "served"}); err != nil {
		t.Fatalf("Write live IDs: %v", err)
	}
	if err := Write(app, Entry{Level: 1, Src: "fetch",
		Message: "GET /api/v1/env/x/config reason=served status=200",
		Project: "dangling-project-id"}); err == nil {
		t.Error("Write dangling project ID = nil, want validation error")
	}

	for _, tc := range []struct {
		name  string
		query LogQuery
		want  int
	}{
		{"env isolates the row", LogQuery{Level: -1, Env: env.Id}, 1},
		{"project isolates the row", LogQuery{Level: -1, Project: proj.Id}, 1},
		{"env plus project narrows to one", LogQuery{Level: -1, Env: env.Id, Project: proj.Id}, 1},
		{"unknown env matches nothing", LogQuery{Level: -1, Env: "000000000000000"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recs, err := Query(app, tc.query, 1, 50)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(recs) != tc.want {
				t.Errorf("Query = %d rows, want %d", len(recs), tc.want)
			}
			if int64(len(recs)) != countLogs(t, app, tc.query) {
				t.Errorf("page (%d) and total disagree", len(recs))
			}
		})
	}
}
