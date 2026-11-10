package migrations

// Same-package migration test (convention: 1790000028_rollup_state_test.go).
// tests.NewTestApp boots with every registered migration applied, so the
// app starts in the POST-migration state; the down/up helpers are called
// directly to prove reversibility. Style: stdlib testing,
// Given/When/Then, fresh app per test, no sleep, -race/-shuffle safe.

import (
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

func newAuditLogApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

func mustAuditLogCollection(t *testing.T, app *tests.TestApp) *core.Collection {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(auditLogCollectionName)
	if err != nil {
		t.Fatalf("find %s: %v", auditLogCollectionName, err)
	}
	return col
}

// assertAuditLogShape pins audit_log{id cw_audit_log, actor/action
// presentable, action required, status Number OnlyInt, detail JSON,
// created/updated Autodate}, nil rules, and the four indexes.
func assertAuditLogShape(t *testing.T, app *tests.TestApp) {
	t.Helper()
	col := mustAuditLogCollection(t, app)

	if col.Id != auditLogCollectionID {
		t.Errorf("audit_log id = %q, want %q", col.Id, auditLogCollectionID)
	}
	actor, ok := col.Fields.GetByName("actor").(*core.TextField)
	if !ok {
		t.Fatalf("audit_log.actor is %T, want *core.TextField", col.Fields.GetByName("actor"))
	}
	if !actor.Presentable {
		t.Error("audit_log.actor Presentable = false, want true")
	}
	if _, ok := col.Fields.GetByName("actorId").(*core.TextField); !ok {
		t.Fatalf("audit_log.actorId is %T, want *core.TextField", col.Fields.GetByName("actorId"))
	}
	action, ok := col.Fields.GetByName("action").(*core.TextField)
	if !ok {
		t.Fatalf("audit_log.action is %T, want *core.TextField", col.Fields.GetByName("action"))
	}
	if !action.Required {
		t.Error("audit_log.action Required = false, want true")
	}
	if !action.Presentable {
		t.Error("audit_log.action Presentable = false, want true")
	}
	for _, name := range []string{"targetType", "targetId", "targetLabel"} {
		if _, ok := col.Fields.GetByName(name).(*core.TextField); !ok {
			t.Errorf("audit_log.%s is %T, want *core.TextField", name, col.Fields.GetByName(name))
		}
	}
	status, ok := col.Fields.GetByName("status").(*core.NumberField)
	if !ok {
		t.Fatalf("audit_log.status is %T, want *core.NumberField", col.Fields.GetByName("status"))
	}
	if !status.OnlyInt {
		t.Error("audit_log.status OnlyInt = false, want true")
	}
	if _, ok := col.Fields.GetByName("detail").(*core.JSONField); !ok {
		t.Fatalf("audit_log.detail is %T, want *core.JSONField", col.Fields.GetByName("detail"))
	}
	for _, name := range []string{"created", "updated"} {
		ad, ok := col.Fields.GetByName(name).(*core.AutodateField)
		if !ok {
			t.Fatalf("audit_log.%s is %T, want *core.AutodateField", name, col.Fields.GetByName(name))
		}
		if !ad.OnCreate {
			t.Errorf("audit_log.%s OnCreate = false, want true", name)
		}
	}
	if col.ListRule != nil || col.ViewRule != nil || col.CreateRule != nil ||
		col.UpdateRule != nil || col.DeleteRule != nil {
		t.Errorf("audit_log rules = (%v,%v,%v,%v,%v), want all nil",
			col.ListRule, col.ViewRule, col.CreateRule, col.UpdateRule, col.DeleteRule)
	}

	for _, want := range []struct {
		index, columns string
	}{
		{"idx_audit_created", "created"},
		{"idx_audit_action", "action"},
		{"idx_audit_actor", "actor"},
		{"idx_audit_target", "targetType, targetId"},
	} {
		idx := col.GetIndex(want.index)
		if idx == "" {
			t.Fatalf("audit_log missing %s", want.index)
		}
		if indexIsUnique(idx) {
			t.Errorf("audit_log.%s = %q, want non-unique", want.index, idx)
		}
		if !strings.Contains(idx, want.columns) {
			t.Errorf("audit_log.%s = %q, want columns %q", want.index, idx, want.columns)
		}
	}
}

// TestAuditLogUpShape pins the post-migration schema.
func TestAuditLogUpShape(t *testing.T) {
	app := newAuditLogApp(t)
	assertAuditLogShape(t, app)
}

// TestAuditLogUpIdempotent pins that rerunning up converges.
func TestAuditLogUpIdempotent(t *testing.T) {
	app := newAuditLogApp(t)
	if err := upAuditLog(app); err != nil {
		t.Fatalf("upAuditLog rerun: %v", err)
	}
	assertAuditLogShape(t, app)
}

// TestAuditLogDownUp proves down drops the collection and up recreates the
// same shape.
func TestAuditLogDownUp(t *testing.T) {
	app := newAuditLogApp(t)

	if err := downAuditLog(app); err != nil {
		t.Fatalf("downAuditLog: %v", err)
	}
	if _, err := app.FindCollectionByNameOrId(auditLogCollectionName); err == nil {
		t.Fatal("audit_log still present after down")
	}
	// Down is idempotent.
	if err := downAuditLog(app); err != nil {
		t.Fatalf("downAuditLog rerun: %v", err)
	}

	if err := upAuditLog(app); err != nil {
		t.Fatalf("upAuditLog after down: %v", err)
	}
	assertAuditLogShape(t, app)
}
