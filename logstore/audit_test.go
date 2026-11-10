package logstore

// Audit trail wiring: PUT /api/v1/admin/log-settings emits one
// log_settings.update row with the non-secret knobs only.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/internal/testhelp"
)

func TestAuditLogSettingsUpdate(t *testing.T) {
	app := settingsTestApp(t)
	testhelp.EnsureAuditCollection(t, app)
	resetForTest(Defaults())

	body := `{"enabled":true,"successLogs":false,"maxDays":14,"adminLogs":{"enabled":true,"successLines":false,"healthChecks":true}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/log-settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := putLogSettings(re); err != nil {
		code, msg := testhelp.CodeMessage(err, rec)
		t.Fatalf("putLogSettings = %d %q", code, msg)
	}

	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": "log_settings.update"})
	if err != nil {
		t.Fatalf("find audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("log_settings.update rows = %d, want 1", len(rows))
	}
	raw, _ := json.Marshal(rows[0].GetRaw("detail"))
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if d["maxDays"] != float64(14) || d["successLogs"] != false || d["enabled"] != true {
		t.Errorf("detail = %v", d)
	}
	admin, _ := d["adminLogs"].(map[string]any)
	if admin["enabled"] != true || admin["successLines"] != false || admin["healthChecks"] != true {
		t.Errorf("adminLogs detail = %v", admin)
	}
}
