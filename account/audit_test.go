package account

// Audit trail wiring: every account/session action emits exactly one
// audit_log row with the expected action, target and status. Same
// package, existing handler helpers reused.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/internal/testhelp"
)

func auditRows(t *testing.T, app core.App, action string) []*core.Record {
	t.Helper()
	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": action})
	if err != nil {
		t.Fatalf("find audit %s: %v", action, err)
	}
	return rows
}

func assertAuditRow(t *testing.T, app core.App, action string, wantStatus int, wantType, wantLabel string) *core.Record {
	t.Helper()
	rows := auditRows(t, app, action)
	if len(rows) != 1 {
		t.Fatalf("%s rows = %d, want exactly 1", action, len(rows))
	}
	r := rows[0]
	if got := r.GetInt("status"); got != wantStatus {
		t.Errorf("%s status = %d, want %d", action, got, wantStatus)
	}
	if wantType != "" {
		if got := r.GetString("targetType"); got != wantType {
			t.Errorf("%s targetType = %q, want %q", action, got, wantType)
		}
	}
	if wantLabel != "" {
		if got := r.GetString("targetLabel"); got != wantLabel {
			t.Errorf("%s targetLabel = %q, want %q", action, got, wantLabel)
		}
	}
	return r
}

func callCreateAccount(app core.App, email string) int {
	body := strings.NewReader(`{"email":"` + email + `","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account/create", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := createAccount(re); err != nil {
		return testhelp.Code(err, rec)
	}
	return rec.Code
}

func TestAuditAccountAndSessionActions(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	testhelp.EnsureAuditCollection(t, app)

	if code := callPostSetup(app, "first@example.com"); code != http.StatusCreated {
		t.Fatalf("postSetup = %d, want 201", code)
	}
	assertAuditRow(t, app, "setup.claim", http.StatusCreated, "account", "first@example.com")

	if code := callCreateAccount(app, "second@example.com"); code != http.StatusCreated {
		t.Fatalf("createAccount = %d, want 201", code)
	}
	second := assertAuditRow(t, app, "account.create", http.StatusCreated, "account", "second@example.com")

	if code, msg := callSetPassword(app, second.GetString("targetId"), "another-password"); code != http.StatusOK {
		t.Fatalf("setPassword = %d %q, want 200", code, msg)
	}
	assertAuditRow(t, app, "account.password", http.StatusOK, "account", "second@example.com")

	if code, msg := callDeleteAccount(app, second.GetString("targetId")); code != http.StatusOK {
		t.Fatalf("deleteAccount = %d %q, want 200", code, msg)
	}
	assertAuditRow(t, app, "account.delete", http.StatusOK, "account", "second@example.com")

	if code, _, _ := callPostSession(app, `{"email":"ghost@example.com","password":"password123"}`); code != http.StatusUnauthorized {
		t.Fatalf("failed login = %d, want 401", code)
	}
	assertAuditRow(t, app, "session.login_failed", http.StatusUnauthorized, "account", "ghost@example.com")

	if code, _, _ := callPostSession(app, `{"email":"first@example.com","password":"password123"}`); code != http.StatusOK {
		t.Fatalf("login = %d, want 200", code)
	}
	login := assertAuditRow(t, app, "session.login", http.StatusOK, "account", "first@example.com")
	if login.GetString("actor") != "first@example.com" {
		t.Errorf("session.login actor = %q, want first@example.com", login.GetString("actor"))
	}

	rec := findSuperuserByEmail(app, "first@example.com")
	if rec == nil {
		t.Fatal("find superuser: nil")
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/session", nil)
	recOrder := httptest.NewRecorder()
	re := &core.RequestEvent{App: app, Auth: rec}
	re.Request = req
	re.Response = recOrder
	if err := deleteSession(re); err != nil {
		t.Fatalf("deleteSession: %v", err)
	}
	logout := assertAuditRow(t, app, "session.logout", http.StatusOK, "account", "first@example.com")
	if logout.GetString("actor") != "first@example.com" {
		t.Errorf("session.logout actor = %q, want first@example.com", logout.GetString("actor"))
	}
}

// TestLoginFailureAuditThrottle pins the public-endpoint bound: a repeated
// failed login from one IP writes one audit row per window, while a different
// IP still gets its own.
func TestLoginFailureAuditThrottle(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	testhelp.EnsureAuditCollection(t, app)

	failure := func(ip, email string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session", nil)
		req.RemoteAddr = ip + ":1234"
		re := &core.RequestEvent{App: app}
		re.Request = req
		re.Response = httptest.NewRecorder()
		auditLoginFailure(re, email)
	}

	failure("203.0.113.7", "a@example.com")
	failure("203.0.113.7", "b@example.com")
	if rows := auditRows(t, app, "session.login_failed"); len(rows) != 1 {
		t.Fatalf("login_failed rows for one IP = %d, want 1", len(rows))
	}
	failure("198.51.100.9", "c@example.com")
	if rows := auditRows(t, app, "session.login_failed"); len(rows) != 2 {
		t.Fatalf("login_failed rows after a second IP = %d, want 2", len(rows))
	}
}
