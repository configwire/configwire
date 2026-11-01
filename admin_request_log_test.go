package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"

	"github.com/configwire/configwire/logstore"
)

// adminLogTestApp boots a full-migration TestApp (server_logs and
// log_settings present) and restores the logstore defaults on cleanup.
func adminLogTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() {
		_ = logstore.UpdateSettings(app, logstore.Defaults())
		app.Cleanup()
	})
	return app
}

// adminLogEvent builds a bare RequestEvent for the middleware: Next is
// the hook no-op, so the handler writes nothing and the audit line
// defaults to 200.
func adminLogEvent(app core.App, method, target string) (*core.RequestEvent, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	return re, rec
}

func setAdminLogArea(t *testing.T, app core.App, cfg logstore.AdminLogSettings) {
	t.Helper()
	full := logstore.GetConfig()
	full.AdminLogs = cfg
	if err := logstore.UpdateSettings(app, full); err != nil {
		t.Fatalf("UpdateSettings adminLogs=%+v: %v", cfg, err)
	}
}

var adminLogsOn = logstore.AdminLogSettings{Enabled: true, SuccessLines: true, HealthChecks: false}
var adminLogsOff = logstore.AdminLogSettings{Enabled: false, SuccessLines: true, HealthChecks: false}

func countAdminLines(t *testing.T, app core.App) int {
	t.Helper()
	n, err := app.CountRecords("server_logs")
	if err != nil {
		t.Fatalf("count server_logs: %v", err)
	}
	return int(n)
}

// TestLogAdminRequestsOffWritesNothing pins that the default-off area
// leaves server_logs untouched for admin paths.
func TestLogAdminRequestsOffWritesNothing(t *testing.T) {
	app := adminLogTestApp(t)
	setAdminLogArea(t, app, adminLogsOff)

	re, _ := adminLogEvent(app, http.MethodGet, "/api/v1/admin/logs?page=1")
	if err := logAdminRequests(re); err != nil {
		t.Fatalf("logAdminRequests: %v", err)
	}
	if n := countAdminLines(t, app); n != 0 {
		t.Fatalf("server_logs rows = %d, want 0 (area off)", n)
	}
}

// TestLogAdminRequestsOnLogsAdminPath pins that an enabled area
// records one audit line with method, path (no query), status, and the
// admin-request reason.
func TestLogAdminRequestsOnLogsAdminPath(t *testing.T) {
	app := adminLogTestApp(t)
	setAdminLogArea(t, app, adminLogsOn)

	re, _ := adminLogEvent(app, http.MethodGet, "/api/v1/admin/logs?page=1&perPage=50")
	if err := logAdminRequests(re); err != nil {
		t.Fatalf("logAdminRequests: %v", err)
	}
	if n := countAdminLines(t, app); n != 1 {
		t.Fatalf("server_logs rows = %d, want 1", n)
	}
	recs, err := app.FindRecordsByFilter("server_logs", "", "", 1, 0, nil)
	if err != nil || len(recs) != 1 {
		t.Fatalf("find server_logs: %v %d", err, len(recs))
	}
	rec := recs[0]
	if got := rec.GetString("message"); got != "GET /api/v1/admin/logs -> 200" {
		t.Errorf("message = %q, want path-only audit line", got)
	}
	if strings.Contains(rec.GetString("message"), "?") || strings.Contains(rec.GetString("message"), "perPage") {
		t.Errorf("message = %q, want no query string", rec.GetString("message"))
	}
	if got := rec.GetString("reason"); got != "admin-request" {
		t.Errorf("reason = %q, want admin-request", got)
	}
	if got := rec.GetString("src"); got != "admin" {
		t.Errorf("src = %q, want admin", got)
	}
	if got := rec.GetInt("status"); got != 200 {
		t.Errorf("status = %d, want 200", got)
	}
}

// TestLogAdminRequestsSkipsNonAdmin pins that SDK/data paths never reach
// the audit store even with the area on.
func TestLogAdminRequestsSkipsNonAdmin(t *testing.T) {
	app := adminLogTestApp(t)
	setAdminLogArea(t, app, adminLogsOn)

	for _, target := range []string{
		"/api/v1/env/prod/config?b=123",
		"/api/v1/admin/setup/status",
		"/healthz",
	} {
		// /api/v1/admin/setup/status IS an admin path (logged); the
		// others must not be (/healthz needs the healthChecks knob).
		re, _ := adminLogEvent(app, http.MethodGet, target)
		if err := logAdminRequests(re); err != nil {
			t.Fatalf("logAdminRequests %s: %v", target, err)
		}
	}
	if n := countAdminLines(t, app); n != 1 {
		t.Fatalf("server_logs rows = %d, want 1 (only the admin path)", n)
	}
}

// TestLogAdminRequestsSuccessLinesOffSkips2xx pins the success-lines
// knob: with the area on but success lines off, the default-200 audit
// line is skipped.
func TestLogAdminRequestsSuccessLinesOffSkips2xx(t *testing.T) {
	app := adminLogTestApp(t)
	setAdminLogArea(t, app, logstore.AdminLogSettings{Enabled: true, SuccessLines: false, HealthChecks: false})

	re, _ := adminLogEvent(app, http.MethodGet, "/api/v1/admin/logs")
	if err := logAdminRequests(re); err != nil {
		t.Fatalf("logAdminRequests: %v", err)
	}
	if n := countAdminLines(t, app); n != 0 {
		t.Fatalf("server_logs rows = %d, want 0 (2xx skipped)", n)
	}
}

// TestLogAdminRequestsHealthChecks pins the health-check knob: /healthz
// is skipped by default, recorded with its own src/reason when on.
func TestLogAdminRequestsHealthChecks(t *testing.T) {
	app := adminLogTestApp(t)
	setAdminLogArea(t, app, adminLogsOn)

	re, _ := adminLogEvent(app, http.MethodGet, "/healthz")
	if err := logAdminRequests(re); err != nil {
		t.Fatalf("logAdminRequests: %v", err)
	}
	if n := countAdminLines(t, app); n != 0 {
		t.Fatalf("server_logs rows = %d, want 0 (health checks off)", n)
	}

	setAdminLogArea(t, app, logstore.AdminLogSettings{Enabled: true, SuccessLines: true, HealthChecks: true})
	re, _ = adminLogEvent(app, http.MethodGet, "/healthz")
	if err := logAdminRequests(re); err != nil {
		t.Fatalf("logAdminRequests: %v", err)
	}
	if n := countAdminLines(t, app); n != 1 {
		t.Fatalf("server_logs rows = %d, want 1", n)
	}
	recs, err := app.FindRecordsByFilter("server_logs", "", "", 1, 0, nil)
	if err != nil || len(recs) != 1 {
		t.Fatalf("find server_logs: %v %d", err, len(recs))
	}
	rec := recs[0]
	if got := rec.GetString("message"); got != "GET /healthz -> 200" {
		t.Errorf("message = %q", got)
	}
	if got := rec.GetString("reason"); got != "health-check" {
		t.Errorf("reason = %q, want health-check", got)
	}
	if got := rec.GetString("src"); got != "health" {
		t.Errorf("src = %q, want health", got)
	}
}

// TestAdminAuditEntryShape pins the pure line builder: method + path +
// status, info level, caller-supplied src/reason, and no room for a
// query string.
func TestAdminAuditEntryShape(t *testing.T) {
	e := adminAuditEntry("admin", http.MethodDelete, "/api/v1/admin/session", 200, "admin-request")
	if e.Message != "DELETE /api/v1/admin/session -> 200" {
		t.Errorf("message = %q", e.Message)
	}
	if e.Level != 1 || e.Src != "admin" || e.Reason != "admin-request" || e.Status != 200 {
		t.Errorf("entry = %+v, want info/admin/admin-request/200", e)
	}
}
