package retention

// Audit trail wiring: PUT /api/v1/admin/retention emits one
// retention.update row with both windows.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"github.com/configwire/configwire/internal/testhelp"
)

func retentionAuditApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() {
		ResetForTest(Defaults())
		app.Cleanup()
	})
	col := core.NewBaseCollection("retention_settings")
	col.Fields.Add(
		&core.TextField{Name: "key", Required: true},
		&core.NumberField{Name: "rawDays"},
		&core.NumberField{Name: "rollupDays"},
	)
	if err := app.Save(col); err != nil {
		t.Fatalf("save retention_settings: %v", err)
	}
	testhelp.EnsureAuditCollection(t, app)
	return app
}

func TestAuditRetentionUpdate(t *testing.T) {
	app := retentionAuditApp(t)
	ResetForTest(Defaults())

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/retention", strings.NewReader(`{"retentionDays":120}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := putRetention(re); err != nil {
		code, msg := testhelp.CodeMessage(err, rec)
		t.Fatalf("putRetention = %d %q", code, msg)
	}

	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": "retention.update"})
	if err != nil {
		t.Fatalf("find audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("retention.update rows = %d, want 1", len(rows))
	}
	raw, _ := json.Marshal(rows[0].GetRaw("detail"))
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if d["retentionDays"] != float64(120) || d["rawDays"] != float64(30) || d["rollupDays"] != float64(120) {
		t.Errorf("detail = %v", d)
	}
}
