package purge

// Audit trail wiring: the manual purge route reports auditLogs on both
// dry and live runs and records one purge.run row; the daily ticker path
// purges outdated audit rows.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"github.com/configwire/configwire/audit"
	"github.com/configwire/configwire/internal/testhelp"
)

func purgeAuditTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app := serverLogsTestApp(t)
	testhelp.EnsureAuditCollection(t, app)
	return app
}

func seedAuditRow(t *testing.T, app *tests.TestApp, created time.Time) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("audit_log")
	if err != nil {
		t.Fatalf("find audit_log: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("actor", "admin@example.com")
	rec.Set("action", "session.login")
	rec.Set("targetType", "account")
	rec.Set("targetLabel", "admin@example.com")
	rec.Set("status", 200)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save audit row: %v", err)
	}
	_, err = app.DB().NewQuery("UPDATE audit_log SET created = {:ts} WHERE id = {:id}").
		Bind(map[string]any{"ts": created.UTC().Format("2006-01-02 15:04:05.000Z"), "id": rec.Id}).
		Execute()
	if err != nil {
		t.Fatalf("backdate audit row: %v", err)
	}
	return rec.Id
}

func postPurgeCall(app core.App, query string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/maintenance/purge"+query, nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postPurge(re); err != nil {
		code, _ := testhelp.CodeMessage(err, rec)
		return code, nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		return rec.Code, nil
	}
	return rec.Code, out
}

func auditActionRows(t *testing.T, app core.App, action string) []*core.Record {
	t.Helper()
	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": action})
	if err != nil {
		t.Fatalf("find audit %s: %v", action, err)
	}
	return rows
}

func TestAuditPurgeDryAndLive(t *testing.T) {
	app := purgeAuditTestApp(t)
	now := time.Now().UTC()
	oldID := seedAuditRow(t, app, now.AddDate(0, 0, -(audit.GetConfig().MaxDays+1)))
	freshID := seedAuditRow(t, app, now)

	code, out := postPurgeCall(app, "?dry=1")
	if code != http.StatusOK || out == nil {
		t.Fatalf("dry purge = %d %v, want 200 JSON", code, out)
	}
	if out["auditLogs"] != float64(1) {
		t.Errorf("dry auditLogs = %v, want 1", out["auditLogs"])
	}
	rows := auditActionRows(t, app, "purge.run")
	if len(rows) != 1 {
		t.Fatalf("purge.run rows after dry = %d, want 1", len(rows))
	}
	raw, _ := json.Marshal(rows[0].GetRaw("detail"))
	if !strings.Contains(string(raw), `"dry":true`) || !strings.Contains(string(raw), `"auditLogs":1`) {
		t.Errorf("dry purge.run detail = %s, want dry=true auditLogs=1", raw)
	}

	code, out = postPurgeCall(app, "")
	if code != http.StatusOK || out == nil {
		t.Fatalf("live purge = %d %v, want 200 JSON", code, out)
	}
	if out["auditLogs"] != float64(1) {
		t.Errorf("live auditLogs = %v, want 1", out["auditLogs"])
	}
	if _, err := app.FindRecordById("audit_log", oldID); err == nil {
		t.Error("outdated audit row survived the live purge")
	}
	if _, err := app.FindRecordById("audit_log", freshID); err != nil {
		t.Errorf("fresh audit row was deleted: %v", err)
	}
	if rows := auditActionRows(t, app, "purge.run"); len(rows) != 2 {
		t.Errorf("purge.run rows after live = %d, want 2 (dry + live)", len(rows))
	}
}
