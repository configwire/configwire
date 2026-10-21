package account

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/configwire/configwire/internal/testhelp"
	"github.com/pocketbase/pocketbase/core"
)

// withSetupToken sets the package setupToken snapshot for the test and
// restores the previous value on cleanup (Register is never called in
// these unit tests, so production can only set it via Register).
func withSetupToken(t *testing.T, v string) {
	t.Helper()
	prev := setupToken
	setupToken = v
	t.Cleanup(func() { setupToken = prev })
}

// callPostSetupTokened is callPostSetupTokenedBody without the body.
func callPostSetupTokened(app core.App, email, headerVal string, setHeader bool) int {
	code, _ := callPostSetupTokenedBody(app, email, headerVal, setHeader)
	return code
}

// callPostSetupTokenedBody is callPostSetupTokened plus the raw recorder
// body, for pinning the 403 generic-error shape.
func callPostSetupTokenedBody(app core.App, email, headerVal string, setHeader bool) (int, string) {
	body := strings.NewReader(`{"email":"` + email + `","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/setup", body)
	req.Header.Set("Content-Type", "application/json")
	if setHeader {
		req.Header.Set(setupTokenHeader, headerVal)
	}
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postSetup(re); err != nil {
		code, msg := testhelp.CodeMessage(err, rec)
		return code, msg + rec.Body.String()
	}
	return rec.Code, rec.Body.String()
}

func TestSetupTokenCorrectHeaderProceeds(t *testing.T) {
	withSetupToken(t, "change-me-32-chars-test-token")
	app := accountTestApp(t)
	if code := callPostSetupTokened(app, "guarded@example.com", "change-me-32-chars-test-token", true); code != http.StatusCreated {
		t.Fatalf("setup with correct token: got status %d, want 201", code)
	}
}

func TestSetupTokenCorrectHeaderAfterSetupConflict(t *testing.T) {
	withSetupToken(t, "change-me-32-chars-test-token")
	app := accountTestApp(t)
	if _, err := createSuperuser(app, "first@example.com", "password123"); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if code := callPostSetupTokened(app, "second@example.com", "change-me-32-chars-test-token", true); code != http.StatusConflict {
		t.Fatalf("setup with correct token after claim: got status %d, want 409", code)
	}
}

func TestSetupTokenMissingHeaderForbidden(t *testing.T) {
	withSetupToken(t, "change-me-32-chars-test-token")
	app := accountTestApp(t)
	code, body := callPostSetupTokenedBody(app, "blocked@example.com", "", false)
	if code != http.StatusForbidden {
		t.Fatalf("setup without token header: got status %d, want 403", code)
	}
	if !strings.Contains(body, "setup requires a valid setup token.") {
		t.Fatalf("403 body = %q, want the generic setup-token message", body)
	}
	if strings.Contains(body, "change-me-32-chars-test-token") {
		t.Fatalf("403 body = %q, must not echo the token value", body)
	}
	if n, err := countRealSuperusers(app); err != nil || n != 0 {
		t.Fatalf("after blocked setup: count=%d err=%v, want 0 (no row created)", n, err)
	}
}

func TestSetupTokenWrongHeaderForbidden(t *testing.T) {
	withSetupToken(t, "change-me-32-chars-test-token")
	app := accountTestApp(t)
	code, body := callPostSetupTokenedBody(app, "blocked@example.com", "wrong-token", true)
	if code != http.StatusForbidden {
		t.Fatalf("setup with wrong token: got status %d, want 403", code)
	}
	if strings.Contains(body, "wrong-token") || strings.Contains(body, "change-me-32-chars-test-token") {
		t.Fatalf("403 body = %q, must not echo token values", body)
	}
	if n, err := countRealSuperusers(app); err != nil || n != 0 {
		t.Fatalf("after blocked setup: count=%d err=%v, want 0 (no row created)", n, err)
	}
}

// Token set: the status endpoint stays public (needsSetup only).
func TestSetupTokenStatusStaysPublic(t *testing.T) {
	withSetupToken(t, "change-me-32-chars-test-token")
	app := accountTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/setup/status", nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getSetupStatus(re); err != nil {
		t.Fatalf("status with token set returned %v, want nil (public)", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status with token set = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "needsSetup") {
		t.Fatalf("status body = %q, want needsSetup", body)
	}
}

// loadSetupToken reads exactly CONFIGWIRE_SETUP_TOKEN (unset = "").
func TestLoadSetupTokenReadsEnv(t *testing.T) {
	t.Setenv(setupTokenEnv, "env-token-value")
	if got := loadSetupToken(); got != "env-token-value" {
		t.Fatalf("loadSetupToken() = %q, want %q", got, "env-token-value")
	}
}
