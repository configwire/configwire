package audit

// Unit tests for the audit trail core (settings, Write, Query, Count,
// Purge, actor helpers). The audit_log collection is built manually here
// (same shape as migration 1790000029) so this package never depends on
// the migrations registry. Style: Given/When/Then, fresh app per test, no
// sleep, -race/-shuffle safe.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

func newAuditTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	resetForTest(Defaults())
	col := core.NewBaseCollection(collectionName)
	col.Fields.Add(
		&core.TextField{Name: "actor", Presentable: true},
		&core.TextField{Name: "actorId"},
		&core.TextField{Name: "action", Required: true, Presentable: true},
		&core.TextField{Name: "targetType"},
		&core.TextField{Name: "targetId"},
		&core.TextField{Name: "targetLabel"},
		&core.NumberField{Name: "status", OnlyInt: true},
		&core.JSONField{Name: "detail"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	if err := app.Save(col); err != nil {
		t.Fatalf("save audit_log collection: %v", err)
	}
	return app
}

func mustWrite(t *testing.T, app *tests.TestApp, e Entry) {
	t.Helper()
	if err := Write(app, e); err != nil {
		t.Fatalf("Write(%s): %v", e.Action, err)
	}
}

func countRows(t *testing.T, app *tests.TestApp) int {
	t.Helper()
	n, err := app.CountRecords(collectionName)
	if err != nil {
		t.Fatalf("CountRecords: %v", err)
	}
	return int(n)
}

// backdate rewrites the autodate column (Set is a noop for autodates) so
// retention and since-bound tests are deterministic.
func backdate(t *testing.T, app *tests.TestApp, id string, ts time.Time) {
	t.Helper()
	_, err := app.DB().NewQuery("UPDATE audit_log SET created = {:ts} WHERE id = {:id}").
		Bind(map[string]any{"ts": ts.UTC().Format("2006-01-02 15:04:05.000Z"), "id": id}).
		Execute()
	if err != nil {
		t.Fatalf("backdate %s: %v", id, err)
	}
}

func TestSettingsDefaultsAndClamp(t *testing.T) {
	// Given: no env overrides.
	t.Setenv("CONFIGWIRE_AUDIT_ENABLED", "")
	t.Setenv("CONFIGWIRE_AUDIT_MAX_DAYS", "")

	// Then: default-on, one year.
	d := Defaults()
	if !d.Enabled || d.MaxDays != 365 {
		t.Fatalf("Defaults = %+v, want {true 365}", d)
	}

	// When: an out-of-range env value is loaded.
	t.Setenv("CONFIGWIRE_AUDIT_MAX_DAYS", "9999")
	if got := loadDefaultsFromEnv(); got.MaxDays != DefaultMaxDays {
		t.Errorf("clamped maxDays = %d, want %d", got.MaxDays, DefaultMaxDays)
	}

	// When: a disabled env value is loaded.
	t.Setenv("CONFIGWIRE_AUDIT_ENABLED", "false")
	if got := loadDefaultsFromEnv(); got.Enabled {
		t.Error("CONFIGWIRE_AUDIT_ENABLED=false did not disable")
	}
}

func TestDisabledWritesNothing(t *testing.T) {
	app := newAuditTestApp(t)
	resetForTest(Settings{Enabled: false, MaxDays: 365})

	if err := Write(app, Entry{Action: "key.mint", Status: 201}); err != nil {
		t.Fatalf("Write while disabled: %v", err)
	}
	if n := countRows(t, app); n != 0 {
		t.Errorf("rows while disabled = %d, want 0", n)
	}
}

func TestWriteRequiresAction(t *testing.T) {
	app := newAuditTestApp(t)
	if err := Write(app, Entry{Actor: "admin@example.com"}); err == nil {
		t.Fatal("Write without action = nil, want error")
	}
	if n := countRows(t, app); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestWritePersistsEntry(t *testing.T) {
	app := newAuditTestApp(t)
	mustWrite(t, app, Entry{
		Actor: "admin@example.com", ActorID: "u1", Action: "release.publish",
		TargetType: "environment", TargetID: "e1", TargetLabel: "production",
		Status: 200,
		Detail: map[string]any{"env": "production", "version": 3, "etag": "abc"},
	})

	items, total, err := Query(app, Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("Query = %d items / %d total, want 1/1", len(items), total)
	}
	r := items[0]
	if got := r.GetString("actor"); got != "admin@example.com" {
		t.Errorf("actor = %q", got)
	}
	if got := r.GetString("action"); got != "release.publish" {
		t.Errorf("action = %q", got)
	}
	if got := r.GetInt("status"); got != 200 {
		t.Errorf("status = %d", got)
	}
	raw, _ := json.Marshal(r.GetRaw("detail"))
	if !strings.Contains(string(raw), `"version":3`) {
		t.Errorf("detail = %s, want version 3", raw)
	}
}

func TestWriteDefaultsActorToSystem(t *testing.T) {
	app := newAuditTestApp(t)
	mustWrite(t, app, Entry{Action: "purge.run", Status: 200})
	items, _, err := Query(app, Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(items) != 1 || items[0].GetString("actor") != actorSystem {
		t.Fatalf("actor fallback = %q, want %q", items[0].GetString("actor"), actorSystem)
	}
}

func TestActorHelpers(t *testing.T) {
	app := newAuditTestApp(t)

	// Given: no auth.
	if got, id := ActorFromRequest(nil); got != actorSystem || id != "" {
		t.Errorf("ActorFromRequest(nil) = (%q,%q)", got, id)
	}
	if got, id := ActorFromRequestInfo(nil); got != actorSystem || id != "" {
		t.Errorf("ActorFromRequestInfo(nil) = (%q,%q)", got, id)
	}

	// Given: a superuser auth record.
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("find _superusers: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail("admin@example.com")
	rec.SetPassword("correct-horse-battery")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save superuser: %v", err)
	}

	re := &core.RequestEvent{Auth: rec}
	re.App = app
	if got, id := ActorFromRequest(re); got != "admin@example.com" || id != rec.Id {
		t.Errorf("ActorFromRequest = (%q,%q), want (%q,%q)", got, id, "admin@example.com", rec.Id)
	}
	if got, id := ActorFromRequestInfo(&core.RequestInfo{Auth: rec}); got != "admin@example.com" || id != rec.Id {
		t.Errorf("ActorFromRequestInfo = (%q,%q)", got, id)
	}
}

func TestRecordLogsAndIgnoresWriteFailure(t *testing.T) {
	// Given: the collection is missing (migration not applied).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	resetForTest(Defaults())

	// Then: RecordApp never panics and never returns an error to the caller.
	RecordApp(app, Entry{Action: "session.login", Status: 200})
}

func TestQueryFiltersAndPager(t *testing.T) {
	app := newAuditTestApp(t)
	rows := []Entry{
		{Actor: "a@example.com", Action: "session.login", TargetType: "account", TargetLabel: "a@example.com", Status: 200},
		{Actor: "a@example.com", Action: "key.mint", TargetType: "sdk_key", TargetID: "k1", TargetLabel: "cw-AAAA1111", Status: 201},
		{Actor: "a@example.com", Action: "key.mint", TargetType: "sdk_key", TargetID: "k2", TargetLabel: "cw-BBBB2222", Status: 201},
		{Actor: "b@example.com", Action: "session.login_failed", TargetType: "account", TargetLabel: "b@example.com", Status: 401},
	}
	for _, e := range rows {
		mustWrite(t, app, e)
	}

	for _, tc := range []struct {
		name string
		f    Filter
		want int
	}{
		{"all", Filter{}, 4},
		{"action", Filter{Action: "key.mint"}, 2},
		{"actor", Filter{Actor: "b@example.com"}, 1},
		{"target type", Filter{TargetType: "sdk_key"}, 2},
		{"target id", Filter{TargetID: "k1"}, 1},
		{"status", Filter{Status: 401}, 1},
		{"unknown action", Filter{Action: "nope"}, 0},
		{"page 1 of 2", Filter{PerPage: 2, Page: 1}, 2},
		{"page 2 of 2", Filter{PerPage: 2, Page: 2}, 2},
		{"page past end", Filter{PerPage: 2, Page: 9}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := Query(app, tc.f)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(items) != tc.want {
				t.Errorf("items = %d, want %d", len(items), tc.want)
			}
			if total != 4 && tc.f.Action == "" && tc.f.Actor == "" && tc.f.TargetType == "" &&
				tc.f.TargetID == "" && tc.f.Status == 0 && tc.f.SinceDays == 0 {
				t.Errorf("total = %d, want 4", total)
			}
		})
	}
}

func TestQuerySinceBound(t *testing.T) {
	app := newAuditTestApp(t)
	mustWrite(t, app, Entry{Action: "session.login", Status: 200})
	oldID := seedAndID(t, app, "purge.run")

	backdate(t, app, oldID, time.Now().UTC().AddDate(0, 0, -10))

	if n := len(queryAll(t, app, Filter{SinceDays: 7})); n != 1 {
		t.Errorf("since 7d rows = %d, want 1", n)
	}
	if n := len(queryAll(t, app, Filter{SinceDays: 0})); n != 2 {
		t.Errorf("since unbounded rows = %d, want 2", n)
	}
}

func mustCollection(t *testing.T, app *tests.TestApp) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatalf("find %s: %v", collectionName, err)
	}
	return col
}

func seedAndID(t *testing.T, app *tests.TestApp, action string) string {
	t.Helper()
	col := mustCollection(t, app)
	rec := core.NewRecord(col)
	rec.Set("action", action)
	rec.Set("status", 200)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed %s: %v", action, err)
	}
	return rec.Id
}

func queryAll(t *testing.T, app *tests.TestApp, f Filter) []*core.Record {
	t.Helper()
	items, _, err := Query(app, f)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return items
}

func TestCountAndPurgeRetention(t *testing.T) {
	app := newAuditTestApp(t)
	resetForTest(Settings{Enabled: true, MaxDays: 1})

	oldID := seedAndID(t, app, "session.login")
	olderID := seedAndID(t, app, "account.create")
	seedAndID(t, app, "session.logout")
	backdate(t, app, oldID, time.Now().UTC().AddDate(0, 0, -2))
	backdate(t, app, olderID, time.Now().UTC().AddDate(0, 0, -3))

	got, err := Count(app)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}

	deleted, err := Purge(app)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if deleted != 2 {
		t.Errorf("Purge = %d, want 2", deleted)
	}
	if n := countRows(t, app); n != 1 {
		t.Errorf("remaining rows = %d, want 1", n)
	}

	// A second purge converges (nothing doomed).
	if deleted, err := Purge(app); err != nil || deleted != 0 {
		t.Errorf("second Purge = (%d,%v), want (0,nil)", deleted, err)
	}
}
