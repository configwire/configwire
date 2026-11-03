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

// callDeleteSession invokes deleteSession with an optional cookie,
// reporting status plus body.
func callDeleteSession(app core.App, cookie string) (int, *httptest.ResponseRecorder, string) {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/session", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	code, _ := testhelp.CodeMessage(deleteSession(re), rec)
	return code, rec, rec.Body.String()
}

// TestSessionLogoutCookieOnly pins the cookie-only logout: DELETE with
// the cookie is 200 {"ok":true} with no server-side row, empty logout
// is still 200, and the logged-out token stays valid as a bearer until
// its own collection auth-token expiry (no denylist: replay succeeds).
func TestSessionLogoutCookieOnly(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	const email = "logout-cookie@example.com"
	if _, err := createSuperuser(app, email, "password123"); err != nil {
		t.Fatalf("create superuser: %v", err)
	}
	code, hdr, raw := callPostSession(app, `{"email":"`+email+`","password":"password123"}`)
	if code != http.StatusOK {
		t.Fatalf("session post: got %d %q, want 200", code, raw)
	}
	token := sessionCookieValue(t, hdr)

	code, _, body := callDeleteSession(app, token)
	if code != http.StatusOK {
		t.Fatalf("logout: got %d %q, want 200", code, body)
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("logout: body lacks ok:true in %q", body)
	}

	if code, _, body := callDeleteSession(app, ""); code != http.StatusOK {
		t.Fatalf("empty logout: got %d %q, want 200", code, body)
	} else if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("empty logout: body lacks ok:true in %q", body)
	}

	// Bearer replay after logout succeeds: cookie-only logout clears
	// the browser copy, it does not revoke the token server-side.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account/list", nil)
	req.Header.Set("Authorization", token)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if status := testhelp.Code(CookieAuthMiddleware().Func(re), rec); status != http.StatusOK {
		t.Fatalf("bearer replay after logout: got %d, want 200", status)
	}
	if got := re.Request.Header.Get("Authorization"); got != token {
		t.Fatalf("bearer replay: Authorization = %q, want the token untouched", got)
	}
}

// TestSessionCookieRejectsNonSuperuser pins that a valid non-superuser
// bearer presented via the cw_admin cookie never authenticates as admin:
// the middleware must leave re.Auth unset (not superuser) so downstream
// RequireSuperuserAuth still rejects. Skips when the base users collection
// is absent (split-repo fixture drift).
func TestSessionCookieRejectsNonSuperuser(t *testing.T) {
	app := accountTestApp(t)
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Skipf("users collection absent: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail("regular-user@example.com")
	rec.SetPassword("password123")
	if err := app.Save(rec); err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	token, err := rec.NewAuthToken()
	if err != nil {
		t.Fatalf("mint regular token: %v", err)
	}
	if token == "" {
		t.Fatal("empty regular token")
	}
	// Via cookie.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account/list", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = recorder
	if err := CookieAuthMiddleware().Func(re); err != nil {
		t.Fatalf("cookie middleware: %v", err)
	}
	if re.Auth != nil && re.Auth.IsSuperuser() {
		t.Fatal("non-superuser cookie resolved as superuser Auth")
	}
	// Via explicit Authorization header: passed through untouched, still not admin.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account/list", nil)
	req2.Header.Set("Authorization", token)
	rec2 := httptest.NewRecorder()
	re2 := &core.RequestEvent{App: app}
	re2.Request = req2
	re2.Response = rec2
	if err := CookieAuthMiddleware().Func(re2); err != nil {
		t.Fatalf("header middleware: %v", err)
	}
	if re2.Auth != nil && re2.Auth.IsSuperuser() {
		t.Fatal("non-superuser bearer resolved as superuser Auth")
	}
}

// TestSessionOldTokenShape400 pins the hard remove of the migration-only
// token adoption: a legacy {token} body is rejected with 400 and never
// sets a cookie — even when the token itself is a valid superuser bearer.
// Holders without a cookie re-login with email+password.
func TestSessionOldTokenShape400(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	rec, err := createSuperuser(app, "sess-legacy-admin@example.com", "password123")
	if err != nil {
		t.Fatalf("create superuser: %v", err)
	}
	validToken, err := rec.NewAuthToken()
	if err != nil {
		t.Fatalf("mint superuser token: %v", err)
	}

	for _, body := range []string{
		`{"token":"` + validToken + `"}`,
		`{"token":"invalid-token-for-session-test"}`,
	} {
		code, hdr, raw := callPostSession(app, body)
		if code != http.StatusBadRequest {
			t.Fatalf("legacy token body: got %d %q, want 400", code, raw)
		}
		if joined := strings.Join(hdr["Set-Cookie"], ";"); strings.Contains(joined, sessionCookieName+"=") {
			t.Fatalf("rejected token must not set a cookie in %q", joined)
		}
	}
}

func callRefreshSession(app core.App, cookie string, origin string) (int, http.Header, string) {
	return callRefreshWithHeader(app, cookie, "", origin)
}

func callRefreshWithHeader(app core.App, cookie string, authHeader string, origin string) (int, http.Header, string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session/refresh", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
		req.Host = "example.com"
	}
	req.RemoteAddr = "127.0.0.1:52341"
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	err := refreshSession(re)
	code, _ := testhelp.CodeMessage(err, rec)
	return code, rec.Header(), rec.Body.String()
}

func TestSessionRefreshRoundTrip(t *testing.T) {
	resetSessionBucket()
	app := accountTestApp(t)
	const email = "sess-refresh-admin@example.com"
	if _, err := createSuperuser(app, email, "password123"); err != nil {
		t.Fatalf("create superuser: %v", err)
	}
	code, hdr, raw := callPostSession(app, `{"email":"`+email+`","password":"password123"}`)
	if code != http.StatusOK {
		t.Fatalf("login: got %d %q, want 200", code, raw)
	}
	oldCookie := sessionCookieValue(t, hdr)

	code, hdr, raw = callRefreshSession(app, oldCookie, "")
	if code != http.StatusOK {
		t.Fatalf("refresh: got %d %q, want 200", code, raw)
	}
	if !strings.Contains(raw, `"ok":true`) {
		t.Fatalf("refresh body lacks ok:true in %q", raw)
	}
	if strings.Contains(raw, "token") {
		t.Fatalf("refresh response must not carry a token in %q", raw)
	}
	fresh := sessionCookieValue(t, hdr)
	if fresh == "" {
		t.Fatal("refresh: empty cookie value")
	}
	joined := strings.Join(hdr["Set-Cookie"], ";")
	for _, want := range []string{"HttpOnly", "SameSite=Strict", "Path=/", "Max-Age=259200"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("refresh cookie lacks %q in %q", want, joined)
		}
	}
	rec, err := app.FindAuthRecordByToken(fresh, core.TokenTypeAuth)
	if err != nil || rec == nil || !rec.IsSuperuser() {
		t.Fatalf("refresh token lookup: rec=%v err=%v", rec, err)
	}
	if old, err := app.FindAuthRecordByToken(oldCookie, core.TokenTypeAuth); err != nil || old == nil {
		t.Fatalf("old token must stay valid after refresh (no denylist): rec=%v err=%v", old, err)
	}

	if code, _, body := callRefreshSession(app, "", ""); code != http.StatusUnauthorized {
		t.Fatalf("empty refresh: got %d %q, want 401", code, body)
	}
	if code, _, body := callRefreshSession(app, oldCookie, "https://evil.example"); code != http.StatusForbidden {
		t.Fatalf("cross-origin refresh: got %d %q, want 403", code, body)
	}
	if code, _, body := callRefreshWithHeader(app, "", fresh, ""); code != http.StatusUnauthorized {
		t.Fatalf("header-only refresh: got %d %q, want 401 (cookie-only)", code, body)
	}
}

func TestSessionRefreshRejectsNonSuperuser(t *testing.T) {
	app := accountTestApp(t)
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Skipf("users collection absent: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail("refresh-regular@example.com")
	rec.SetPassword("password123")
	if err := app.Save(rec); err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	token, err := rec.NewAuthToken()
	if err != nil {
		t.Fatalf("mint regular token: %v", err)
	}
	if code, _, body := callRefreshSession(app, token, ""); code != http.StatusUnauthorized {
		t.Fatalf("users-cookie refresh on admin endpoint: got %d %q, want 401", code, body)
	}
}

func TestEnsureSuperuserTokenDuration(t *testing.T) {
	app := accountTestApp(t)
	col, err := app.FindCollectionByNameOrId(superusersCollection)
	if err != nil {
		t.Fatalf("find _superusers: %v", err)
	}
	// Longer operator values are kept: sliding refresh only needs the
	// JWT to outlive the cookie window.
	col.AuthToken.Duration = int64(14 * 24 * 60 * 60)
	if err := app.Save(col); err != nil {
		t.Fatalf("save long duration: %v", err)
	}
	ensureSuperuserTokenDuration(app)
	if col, err = app.FindCollectionByNameOrId(superusersCollection); err != nil {
		t.Fatalf("find _superusers: %v", err)
	} else if col.AuthToken.Duration != int64(14*24*60*60) {
		t.Fatalf("long duration overwritten = %d, want kept at %d", col.AuthToken.Duration, int64(14*24*60*60))
	}
	// Shorter values are raised to the 3d minimum.
	col.AuthToken.Duration = int64(60 * 60)
	if err := app.Save(col); err != nil {
		t.Fatalf("save short duration: %v", err)
	}
	ensureSuperuserTokenDuration(app)
	if col, err = app.FindCollectionByNameOrId(superusersCollection); err != nil {
		t.Fatalf("find _superusers: %v", err)
	} else if col.AuthToken.Duration != int64(sessionMaxAgeSeconds) {
		t.Fatalf("token duration = %d, want %d", col.AuthToken.Duration, sessionMaxAgeSeconds)
	}
}
