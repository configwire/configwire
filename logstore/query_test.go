package logstore

import (
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// logsTestApp builds a TestApp with a minimal server_logs collection
// mirroring migration 1790000024_server_logs (level/message/src plus
// autodates; created drives the newest-first order and purge cutoffs).
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

func seedLog(t *testing.T, app *tests.TestApp, level int, src, message string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("server_logs")
	if err != nil {
		t.Fatalf("find server_logs: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("level", level)
	rec.Set("src", src)
	rec.Set("message", message)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save log: %v", err)
	}
}

// seedLogRows covers new (project-tagged) and old (untagged) rows plus
// a second project and a rejection, so single- and multi-filter cases
// all have signal.
func seedLogRows(t *testing.T, app *tests.TestApp) {
	t.Helper()
	seedLog(t, app, 1, "ingest", "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni project=p1")
	seedLog(t, app, 1, "ingest", "POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni")
	seedLog(t, app, 2, "ingest", "POST /api/v1/env/prod/events reason=rate-limited status=429 key_prefix=cw-OAtni limiter=per-key")
	seedLog(t, app, 1, "fetch", "GET /api/v1/env/dev/config reason=served status=200 key_prefix=cw-ZZZZ project=p2")
}

// countLogs mirrors getLogs: level/src HashExps plus the message-token
// LIKEs, so the test pins page/total parity, not just page contents.
func countLogs(t *testing.T, app *tests.TestApp, q LogQuery) int64 {
	t.Helper()
	var exprs []dbx.Expression
	if q.Level >= 0 {
		exprs = append(exprs, dbx.HashExp{"level": q.Level})
	}
	if q.Src != "" {
		exprs = append(exprs, dbx.HashExp{"src": q.Src})
	}
	exprs = append(exprs, q.MessageExprs()...)
	total, err := app.CountRecords("server_logs", exprs...)
	if err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return total
}

func TestQueryMessageTokenFilters(t *testing.T) {
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
		{"reason plus project narrows to one", LogQuery{Level: -1, Reason: "accepted", Project: "p1"}, 1},
		{"limiter isolates rejection", LogQuery{Level: -1, Limiter: "per-key"}, 1},
		{"src still exact-matches", LogQuery{Level: -1, Src: "fetch"}, 1},
		{"level still exact-matches", LogQuery{Level: 2}, 1},
		{"level plus key combine", LogQuery{Level: 1, Key: "cw-OAtni"}, 2},
		{"unknown project matches nothing", LogQuery{Level: -1, Project: "nope"}, 0},
		{"project plus other project matches nothing", LogQuery{Level: -1, Project: "p1", Key: "cw-ZZZZ"}, 0},
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

func TestQueryTokenPagination(t *testing.T) {
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

func TestMessageTokensWireOrder(t *testing.T) {
	q := LogQuery{Project: "p1", Key: "k", Status: "s", Reason: "r", Limiter: "l"}
	want := [][2]string{
		{"project", "p1"}, {"key_prefix", "k"}, {"status", "s"},
		{"reason", "r"}, {"limiter", "l"},
	}
	got := q.messageTokens()
	if len(got) != len(want) {
		t.Fatalf("messageTokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("messageTokens[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if toks := (LogQuery{Level: -1}).messageTokens(); len(toks) != 0 {
		t.Fatalf("empty query messageTokens = %v, want none", toks)
	}
	if exprs := (LogQuery{Level: -1}).MessageExprs(); len(exprs) != 0 {
		t.Fatalf("empty query MessageExprs = %d exprs, want none", len(exprs))
	}
}
