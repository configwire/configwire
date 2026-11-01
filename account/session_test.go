package account

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/internal/testhelp"
)

// TestSessionPerIPBucket pins the session-login budget: the first
// SessionPerIPLimit attempts from one IP pass the gate, the next
// within the window is rejected, and another IP is unaffected.
func TestSessionPerIPBucket(t *testing.T) {
	resetSessionBucket()
	const ip = "203.0.113.7"
	for i := 0; i < SessionPerIPLimit; i++ {
		if !sessionBucket.allow(ip) {
			t.Fatalf("attempt %d/%d: rejected, want allow", i+1, SessionPerIPLimit)
		}
	}
	if sessionBucket.allow(ip) {
		t.Fatal("over-budget attempt: allowed, want reject")
	}
	if !sessionBucket.allow("198.51.100.9") {
		t.Fatal("other IP: rejected, want allow (per-IP isolation)")
	}
}

// sessionResponse builds a bare RequestEvent over a recorder for the
// Secure-flag tests (setSessionCookie only touches Request/Response).
func sessionResponse(method, target string) (*core.RequestEvent, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{}
	re.Request = req
	re.Response = rec
	return re, rec
}

// TestSessionSecureFlag pins the Secure behavior: plain HTTP omits it
// (local dev keeps working), X-Forwarded-Proto: https from a trusted
// proxy peer sets it (Caddy TLS termination), a public peer's header is
// ignored, and logout clearing emits both variants so scheme-mismatched
// logout still deletes the cookie.
func TestSessionSecureFlag(t *testing.T) {
	re, rec := sessionResponse(http.MethodPost, "/api/v1/admin/session")
	setSessionCookie(re, "tok")
	if joined := rec.Header().Get("Set-Cookie"); strings.Contains(joined, "Secure") {
		t.Fatalf("plain-HTTP cookie must not carry Secure: %q", joined)
	}

	re, rec = sessionResponse(http.MethodPost, "/api/v1/admin/session")
	re.Request.RemoteAddr = "127.0.0.1:52341"
	re.Request.Header.Set("X-Forwarded-Proto", "https")
	setSessionCookie(re, "tok")
	if joined := rec.Header().Get("Set-Cookie"); !strings.Contains(joined, "Secure") {
		t.Fatalf("proxied-https cookie lacks Secure: %q", joined)
	}

	re, rec = sessionResponse(http.MethodPost, "/api/v1/admin/session")
	re.Request.RemoteAddr = "203.0.113.9:52341"
	re.Request.Header.Set("X-Forwarded-Proto", "https")
	setSessionCookie(re, "tok")
	if joined := rec.Header().Get("Set-Cookie"); strings.Contains(joined, "Secure") {
		t.Fatalf("untrusted-peer header must not set Secure: %q", joined)
	}

	re, rec = sessionResponse(http.MethodDelete, "/api/v1/admin/session")
	clearSessionCookie(re)
	joined := strings.Join(rec.Header()["Set-Cookie"], ";")
	if !strings.Contains(joined, sessionCookieName+"=") {
		t.Fatalf("clearing response lacks %s cookie in %q", sessionCookieName, joined)
	}
	if !strings.Contains(joined, "Secure") {
		t.Fatalf("clearing response must include a Secure variant in %q", joined)
	}
}

// callPostSession invokes postSession with a JSON body and reports the
// status plus response headers/body.
func callPostSession(app core.App, body string) (int, http.Header, string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:52341"
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	err := postSession(re)
	code, _ := testhelp.CodeMessage(err, rec)
	return code, rec.Header(), rec.Body.String()
}

// sessionCookieValue extracts the session cookie value from headers.
func sessionCookieValue(t *testing.T, hdr http.Header) string {
	t.Helper()
	for _, c := range hdr["Set-Cookie"] {
		if strings.HasPrefix(c, sessionCookieName+"=") {
			v := strings.TrimPrefix(c, sessionCookieName+"=")
			if i := strings.Index(v, ";"); i >= 0 {
				v = v[:i]
			}
			return v
		}
	}
	t.Fatalf("no %s cookie in %q", sessionCookieName, hdr["Set-Cookie"])
	return ""
}

// TestSessionPasswordRoundTrip pins the steady-state login: password POST
// sets the cw_admin cookie with HttpOnly/SameSite/Path, returns ok:true
// with no token in the body, and the cookie resolves to the superuser.
func TestSessionPasswordRoundTrip(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	const email = "sess-admin@example.com"
	if _, err := createSuperuser(app, email, "password123"); err != nil {
		t.Fatalf("create superuser: %v", err)
	}

	code, hdr, raw := callPostSession(app, `{"email":"`+email+`","password":"password123"}`)
	if code != http.StatusOK {
		t.Fatalf("session post: got %d %q, want 200", code, raw)
	}
	if strings.Contains(raw, "password123") {
		t.Fatal("session: response echoes the password")
	}
	if !strings.Contains(raw, `"ok":true`) {
		t.Fatalf("session: body lacks ok:true in %q", raw)
	}
	if strings.Contains(raw, "token") {
		t.Fatalf("session: response must not carry a token in %q", raw)
	}
	joined := strings.Join(hdr["Set-Cookie"], ";")
	for _, want := range []string{sessionCookieName + "=", "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("session cookie lacks %q in %q", want, joined)
		}
	}
	cookie := sessionCookieValue(t, hdr)
	if cookie == "" {
		t.Fatal("session: empty cookie value")
	}
	rec, err := app.FindAuthRecordByToken(cookie, core.TokenTypeAuth)
	if err != nil || rec == nil {
		t.Fatalf("cookie token lookup: rec=%v err=%v", rec, err)
	}
	if !rec.IsSuperuser() {
		t.Fatal("cookie token: resolved record is not a superuser")
	}
}

// TestSessionTokenModeRejectsNonSuperuser pins the migration-only token
// path: a non-superuser (or invalid) token is rejected with 401 and
// never sets a cookie.
func TestSessionTokenModeRejectsNonSuperuser(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	if _, err := createSuperuser(app, "sess-other-admin@example.com", "password123"); err != nil {
		t.Fatalf("create superuser: %v", err)
	}
	var badToken string
	if col, err := app.FindCollectionByNameOrId("users"); err == nil && col != nil {
		rec := core.NewRecord(col)
		rec.SetEmail("plain-user@example.com")
		rec.SetPassword("password123")
		if err := app.Save(rec); err != nil {
			t.Fatalf("save plain user: %v", err)
		}
		tok, err := rec.NewAuthToken()
		if err != nil {
			t.Fatalf("mint plain-user token: %v", err)
		}
		badToken = tok
	} else {
		badToken = "invalid-token-for-session-test"
	}

	code, hdr, raw := callPostSession(app, `{"token":"`+badToken+`"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("non-superuser token: got %d %q, want 401", code, raw)
	}
	if joined := strings.Join(hdr["Set-Cookie"], ";"); strings.Contains(joined, sessionCookieName+"=") {
		t.Fatalf("rejected token must not set a cookie in %q", joined)
	}
}
