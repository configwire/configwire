package audit

// Data-API hook tests: sdk_keys updates/deletes through
// OnRecordUpdateRequest/OnRecordDeleteRequest emit key.revoke /
// key.unrevoke / key.update / key.delete with the superuser actor, and an
// unauthenticated request audits nothing.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"
	"github.com/configwire/configwire/internal/testhelp"
)

func hooksTestApp(t *testing.T) (*tests.TestApp, *core.Collection) {
	t.Helper()
	app := newAuditTestApp(t)
	col := core.NewBaseCollection("sdk_keys")
	col.Fields.Add(
		&core.TextField{Name: "prefix"},
		&core.TextField{Name: "env"},
		&core.TextField{Name: "hash"},
		&core.BoolField{Name: "revoked"},
		&core.NumberField{Name: "fetchRps", OnlyInt: true},
		&core.NumberField{Name: "ingestRps", OnlyInt: true},
	)
	if err := app.Save(col); err != nil {
		t.Fatalf("save sdk_keys: %v", err)
	}
	RegisterHooks(app)
	return app, col
}

func seedSDKKey(t *testing.T, app *tests.TestApp, col *core.Collection, prefix string, revoked bool, fetchRps, ingestRps int) *core.Record {
	t.Helper()
	rec := core.NewRecord(col)
	rec.Set("prefix", prefix)
	rec.Set("revoked", revoked)
	rec.Set("fetchRps", fetchRps)
	rec.Set("ingestRps", ingestRps)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save sdk key %s: %v", prefix, err)
	}
	return rec
}

func triggerUpdate(t *testing.T, app *tests.TestApp, col *core.Collection, rec *core.Record, auth *core.Record) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/collections/sdk_keys/records/"+rec.Id, nil)
	re := &core.RequestEvent{App: app, Auth: auth}
	re.Request = req
	re.Response = httptest.NewRecorder()
	event := &core.RecordRequestEvent{RequestEvent: re, Record: rec}
	event.Collection = col
	return app.OnRecordUpdateRequest("sdk_keys").Trigger(event, func(e *core.RecordRequestEvent) error {
		return e.App.Save(e.Record)
	})
}

func triggerDelete(t *testing.T, app *tests.TestApp, col *core.Collection, rec *core.Record, auth *core.Record) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/collections/sdk_keys/records/"+rec.Id, nil)
	re := &core.RequestEvent{App: app, Auth: auth}
	re.Request = req
	re.Response = httptest.NewRecorder()
	event := &core.RecordRequestEvent{RequestEvent: re, Record: rec}
	event.Collection = col
	return app.OnRecordDeleteRequest("sdk_keys").Trigger(event, func(e *core.RecordRequestEvent) error {
		return e.App.Delete(e.Record)
	})
}

func hookAuditRows(t *testing.T, app *tests.TestApp, action string) []*core.Record {
	t.Helper()
	rows, _, err := Query(app, Filter{Action: action})
	if err != nil {
		t.Fatalf("Query %s: %v", action, err)
	}
	return rows
}

func TestSDKKeyHooksAudit(t *testing.T) {
	app, col := hooksTestApp(t)
	admin := testhelp.MustSuperuser(t, app, "admin@example.com")

	// revoke
	key := seedSDKKey(t, app, col, "cw-AAAA1111", false, 0, 0)
	key.Set("revoked", true)
	if err := triggerUpdate(t, app, col, key, admin); err != nil {
		t.Fatalf("revoke trigger: %v", err)
	}
	rows := hookAuditRows(t, app, "key.revoke")
	if len(rows) != 1 {
		t.Fatalf("key.revoke rows = %d, want 1", len(rows))
	}
	if got := rows[0].GetString("actor"); got != "admin@example.com" {
		t.Errorf("revoke actor = %q", got)
	}
	if got := rows[0].GetString("targetLabel"); got != "cw-AAAA1111" {
		t.Errorf("revoke targetLabel = %q", got)
	}

	// unrevoke
	key = seedSDKKey(t, app, col, "cw-BBBB2222", true, 0, 0)
	key.Set("revoked", false)
	if err := triggerUpdate(t, app, col, key, admin); err != nil {
		t.Fatalf("unrevoke trigger: %v", err)
	}
	if rows := hookAuditRows(t, app, "key.unrevoke"); len(rows) != 1 {
		t.Fatalf("key.unrevoke rows = %d, want 1", len(rows))
	}

	// revoke plus another field: the row keeps both decisions
	key = seedSDKKey(t, app, col, "cw-HHHH8888", false, 0, 0)
	key.Set("revoked", true)
	key.Set("hash", "cafebabe")
	if err := triggerUpdate(t, app, col, key, admin); err != nil {
		t.Fatalf("revoke+hash trigger: %v", err)
	}
	rows = hookAuditRows(t, app, "key.revoke")
	if len(rows) != 2 {
		t.Fatalf("key.revoke rows after revoke+hash = %d, want 2", len(rows))
	}
	both := false
	for _, r := range rows {
		rawDetail, _ := json.Marshal(r.GetRaw("detail"))
		if strings.Contains(string(rawDetail), `"hash"`) && strings.Contains(string(rawDetail), `"revoked":true`) {
			both = true
		}
	}
	if !both {
		t.Error("revoke+hash detail did not record both the revoke and the changed column")
	}
	for _, r := range rows {
		rawDetail, _ := json.Marshal(r.GetRaw("detail"))
		if strings.Contains(string(rawDetail), "cafebabe") {
			t.Errorf("key.revoke detail leaked the hash value: %s", rawDetail)
		}
	}

	// update limits
	key = seedSDKKey(t, app, col, "cw-CCCC3333", false, 0, 0)
	key.Set("fetchRps", 150)
	key.Set("ingestRps", 75)
	if err := triggerUpdate(t, app, col, key, admin); err != nil {
		t.Fatalf("update trigger: %v", err)
	}
	rows = hookAuditRows(t, app, "key.update")
	if len(rows) != 1 {
		t.Fatalf("key.update rows = %d, want 1", len(rows))
	}

	// update a non-rate field: key.update names the changed column
	key = seedSDKKey(t, app, col, "cw-GGGG7777", false, 0, 0)
	key.Set("hash", "deadbeef")
	if err := triggerUpdate(t, app, col, key, admin); err != nil {
		t.Fatalf("hash update trigger: %v", err)
	}
	rows = hookAuditRows(t, app, "key.update")
	if len(rows) != 2 {
		t.Fatalf("key.update rows after hash change = %d, want 2", len(rows))
	}
	named := false
	for _, r := range rows {
		rawDetail, _ := json.Marshal(r.GetRaw("detail"))
		if strings.Contains(string(rawDetail), `"hash"`) {
			named = true
		}
	}
	if !named {
		t.Error("key.update detail did not name the changed hash column")
	}

	// delete
	key = seedSDKKey(t, app, col, "cw-DDDD4444", false, 0, 0)
	if err := triggerDelete(t, app, col, key, admin); err != nil {
		t.Fatalf("delete trigger: %v", err)
	}
	rows = hookAuditRows(t, app, "key.delete")
	if len(rows) != 1 {
		t.Fatalf("key.delete rows = %d, want 1", len(rows))
	}
	if got := rows[0].GetString("targetLabel"); got != "cw-DDDD4444" {
		t.Errorf("delete targetLabel = %q", got)
	}

	// unauthenticated update: business save succeeds, no audit row
	key = seedSDKKey(t, app, col, "cw-EEEE5555", false, 0, 0)
	key.Set("revoked", true)
	if err := triggerUpdate(t, app, col, key, nil); err != nil {
		t.Fatalf("unauth trigger: %v", err)
	}
	if rows := hookAuditRows(t, app, "key.revoke"); len(rows) != 2 {
		t.Errorf("key.revoke rows after unauth update = %d, want 2", len(rows))
	}

	// no-op update: no row
	key = seedSDKKey(t, app, col, "cw-FFFF6666", false, 0, 0)
	if err := triggerUpdate(t, app, col, key, admin); err != nil {
		t.Fatalf("no-op trigger: %v", err)
	}
	if rows := hookAuditRows(t, app, "key.update"); len(rows) != 2 {
		t.Errorf("key.update rows after no-op = %d, want 2", len(rows))
	}
}

func triggerAuditWrite(t *testing.T, app *tests.TestApp, col *core.Collection, rec *core.Record, auth *core.Record, op string) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/collections/audit_log/records", nil)
	re := &core.RequestEvent{App: app, Auth: auth}
	re.Request = req
	re.Response = httptest.NewRecorder()
	event := &core.RecordRequestEvent{RequestEvent: re, Record: rec}
	event.Collection = col
	switch op {
	case "create":
		return app.OnRecordCreateRequest(collectionName).Trigger(event, func(e *core.RecordRequestEvent) error {
			return e.App.Save(e.Record)
		})
	case "update":
		return app.OnRecordUpdateRequest(collectionName).Trigger(event, func(e *core.RecordRequestEvent) error {
			return e.App.Save(e.Record)
		})
	default:
		return app.OnRecordDeleteRequest(collectionName).Trigger(event, func(e *core.RecordRequestEvent) error {
			return e.App.Delete(e.Record)
		})
	}
}

func assertForbidden(t *testing.T, err error, op string) {
	t.Helper()
	var apiErr *router.ApiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("%s through the data API = %v, want 403", op, err)
	}
}

// TestAuditLogDataAPIIsAppendOnly pins the write guard: superuser requests
// cannot create, rewrite or erase trail rows, while server-side writes still
// land.
func TestAuditLogDataAPIIsAppendOnly(t *testing.T) {
	app, _ := hooksTestApp(t)
	admin := testhelp.MustSuperuser(t, app, "admin@example.com")
	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatalf("find audit_log: %v", err)
	}

	before := countRows(t, app)
	rec := core.NewRecord(col)
	rec.Set("action", "tamper")
	assertForbidden(t, triggerAuditWrite(t, app, col, rec, admin, "create"), "create")
	if got := countRows(t, app); got != before {
		t.Fatalf("rows after denied create = %d, want %d", got, before)
	}

	mustWrite(t, app, Entry{Action: "seed.row"})
	rows, _, err := Query(app, Filter{Action: "seed.row"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("seed row: %v (rows=%d)", err, len(rows))
	}
	seed := rows[0]

	seed.Set("action", "rewritten")
	assertForbidden(t, triggerAuditWrite(t, app, col, seed, admin, "update"), "update")
	assertForbidden(t, triggerAuditWrite(t, app, col, seed, admin, "delete"), "delete")

	survived, err := app.FindRecordById(collectionName, seed.Id)
	if err != nil {
		t.Fatalf("seed row after denied writes: %v", err)
	}
	if got := survived.GetString("action"); got != "seed.row" {
		t.Fatalf("seed action = %q, want the original", got)
	}
}

func recordSpecCollections(t *testing.T, app *tests.TestApp) {
	t.Helper()
	for _, spec := range recordAuditSpecs {
		col := core.NewBaseCollection(spec.collection)
		switch spec.collection {
		case "flags":
			col.Fields.Add(&core.TextField{Name: "key"}, &core.TextField{Name: "value"})
		case "rules":
			col.Fields.Add(&core.TextField{Name: "flag"}, &core.TextField{Name: "value"})
		case "experiments", "projects":
			col.Fields.Add(&core.TextField{Name: "name"}, &core.TextField{Name: "value"})
		case "environments":
			col.Fields.Add(&core.TextField{Name: "slug"}, &core.TextField{Name: "value"})
		}
		if err := app.Save(col); err != nil {
			t.Fatalf("save %s collection: %v", spec.collection, err)
		}
	}
}

func triggerRecordWrite(t *testing.T, app *tests.TestApp, col *core.Collection, rec *core.Record, auth *core.Record, op string) error {
	t.Helper()
	method := http.MethodPost
	switch op {
	case "update":
		method = http.MethodPatch
	case "delete":
		method = http.MethodDelete
	}
	req := httptest.NewRequest(method, "/api/collections/"+col.Name+"/records", nil)
	re := &core.RequestEvent{App: app, Auth: auth}
	re.Request = req
	re.Response = httptest.NewRecorder()
	event := &core.RecordRequestEvent{RequestEvent: re, Record: rec}
	event.Collection = col
	next := func(e *core.RecordRequestEvent) error { return e.App.Save(e.Record) }
	switch op {
	case "create":
		return app.OnRecordCreateRequest(col.Name).Trigger(event, next)
	case "update":
		return app.OnRecordUpdateRequest(col.Name).Trigger(event, next)
	default:
		return app.OnRecordDeleteRequest(col.Name).Trigger(event, func(e *core.RecordRequestEvent) error {
			return e.App.Delete(e.Record)
		})
	}
}

// TestRecordHooksAudit pins the five data-API record hooks: create/update/
// delete each emit one row with the actor, target type/id/label, and an update
// that changes nothing emits none.
func TestRecordHooksAudit(t *testing.T) {
	app, _ := hooksTestApp(t)
	recordSpecCollections(t, app)
	admin := testhelp.MustSuperuser(t, app, "admin@example.com")

	labelField := map[string]string{
		"flags": "key", "rules": "flag", "experiments": "name",
		"projects": "name", "environments": "slug",
	}
	for _, spec := range recordAuditSpecs {
		t.Run(spec.collection, func(t *testing.T) {
			col, err := app.FindCollectionByNameOrId(spec.collection)
			if err != nil {
				t.Fatalf("find %s: %v", spec.collection, err)
			}
			labelValue := "alpha"
			if spec.collection == "rules" {
				labelValue = "flag-1"
			}
			rec := core.NewRecord(col)
			rec.Set(labelField[spec.collection], labelValue)
			rec.Set("value", "one")

			if err := triggerRecordWrite(t, app, col, rec, admin, "create"); err != nil {
				t.Fatalf("create: %v", err)
			}
			createdID := rec.Id
			assertRecordAudit(t, app, spec.targetType+".create", spec.targetType, createdID, labelValue)
			createRow := hookAuditRows(t, app, spec.targetType+".create")[0]
			if got := createRow.GetString("actor"); got != "admin@example.com" {
				t.Errorf("create actor = %q", got)
			}

			rec.Set("value", "two")
			if err := triggerRecordWrite(t, app, col, rec, admin, "update"); err != nil {
				t.Fatalf("update: %v", err)
			}
			assertRecordAudit(t, app, spec.targetType+".update", spec.targetType, createdID, labelValue)
			updateRow := hookAuditRows(t, app, spec.targetType+".update")[0]
			rawDetail, _ := json.Marshal(updateRow.GetRaw("detail"))
			if !strings.Contains(string(rawDetail), `"value"`) {
				t.Errorf("update detail = %s, want the changed field name", rawDetail)
			}

			if err := triggerRecordWrite(t, app, col, rec, admin, "update"); err != nil {
				t.Fatalf("no-op update: %v", err)
			}
			if rows := hookAuditRows(t, app, spec.targetType+".update"); len(rows) != 1 {
				t.Errorf("no-op update rows = %d, want 1", len(rows))
			}

			if err := triggerRecordWrite(t, app, col, rec, admin, "delete"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			assertRecordAudit(t, app, spec.targetType+".delete", spec.targetType, createdID, labelValue)
		})
	}
}

func assertRecordAudit(t *testing.T, app *tests.TestApp, action, targetType, targetID, targetLabel string) {
	t.Helper()
	rows := hookAuditRows(t, app, action)
	if len(rows) != 1 {
		t.Fatalf("%s rows = %d, want 1", action, len(rows))
	}
	r := rows[0]
	if got := r.GetString("targetType"); got != targetType {
		t.Errorf("%s targetType = %q, want %q", action, got, targetType)
	}
	if got := r.GetString("targetId"); got != targetID {
		t.Errorf("%s targetId = %q, want %q", action, got, targetID)
	}
	if got := r.GetString("targetLabel"); got != targetLabel {
		t.Errorf("%s targetLabel = %q, want %q", action, got, targetLabel)
	}
}
