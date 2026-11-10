package limits

// Audit trail wiring: PUT /api/v1/admin/limits emits one limits.update row
// with numeric detail and the allowlist COUNT only.

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

func limitsAuditApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() {
		resetForTest(Defaults())
		app.Cleanup()
	})
	col := core.NewBaseCollection("rate_settings")
	col.Fields.Add(
		&core.TextField{Name: "key", Required: true},
		&core.NumberField{Name: "rps"},
		&core.NumberField{Name: "burst"},
		&core.NumberField{Name: "fetchRps"},
		&core.NumberField{Name: "ingestRps"},
		&core.NumberField{Name: "adminRps"},
		&core.TextField{Name: "ipHeaders"},
		&core.TextField{Name: "adminAllowedIPs"},
		&core.TextField{Name: "countryHeaders"},
	)
	if err := app.Save(col); err != nil {
		t.Fatalf("save rate_settings: %v", err)
	}
	testhelp.EnsureAuditCollection(t, app)
	return app
}

func callPutLimits(app core.App, body string) (int, string) {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/limits", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := putLimits(re); err != nil {
		return testhelp.CodeMessage(err, rec)
	}
	return rec.Code, ""
}

func TestAuditLimitsUpdate(t *testing.T) {
	app := limitsAuditApp(t)
	resetForTest(Defaults())

	code, msg := callPutLimits(app, `{"globalRps":300,"burst":500,"fetchRps":120,"ingestRps":60,"adminAllowedIPs":["10.0.0.1","10.0.0.2"]}`)
	if code != http.StatusOK {
		t.Fatalf("putLimits = %d %q, want 200", code, msg)
	}
	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": "limits.update"})
	if err != nil {
		t.Fatalf("find audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("limits.update rows = %d, want 1", len(rows))
	}
	raw, err := json.Marshal(rows[0].GetRaw("detail"))
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	detail := string(raw)
	for _, forbidden := range []string{"10.0.0.1", "10.0.0.2"} {
		if strings.Contains(detail, forbidden) {
			t.Errorf("audit detail leaked allowlist entry %q: %s", forbidden, detail)
		}
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if d["globalRps"] != float64(300) || d["burst"] != float64(500) ||
		d["fetchRps"] != float64(120) || d["ingestRps"] != float64(60) {
		t.Errorf("detail numeric fields = %v", d)
	}
	if d["adminAllowedIPs"] != float64(2) {
		t.Errorf("adminAllowedIPs = %v, want count 2", d["adminAllowedIPs"])
	}
}
