// Cookie-carried superuser auth: the Admin UI no longer persists the
// superuser token in localStorage. Instead the browser holds it in a
// JS-invisible cookie and the middleware below re-attaches it per
// request. Logout clears the cookie in that browser only: a copied
// token stays usable elsewhere until its own collection auth-token
// expiry. There is no server-side revocation list.
//
//   - POST /api/v1/admin/session (PUBLIC, Origin-checked, per-IP
//     rate-limited) accepts {email,password} only: a legacy {token}
//     body answers 400 (holders without a cookie re-login with the
//     password). On success it
//     sets the cw_admin cookie (HttpOnly, SameSite=Strict, Path=/,
//     Max-Age 3d by default (CONFIGWIRE_SESSION_MAX_AGE to tune);
//     Secure when the request is TLS or the proxy signals
//     https via X-Forwarded-Proto, so plain-HTTP local dev keeps
//     working) and returns {"ok":true} with no token in the body.
//     Direct auth-with-password login is untouched.
//   - POST /api/v1/admin/session/refresh (cookie-authed,
//     Origin-checked) re-mints the token and re-sets the cookie with a
//     fresh window, returning {"ok":true} with no token in the body.
//     Cookie-only equivalent of PocketBase auth-refresh without
//     exposing the native JSON token to JS.
//   - DELETE /api/v1/admin/session clears the cw_admin cookie and
//     returns {"ok":true}. Idempotent; no auth required (logout).
//     Empty logout (no token presented) is still 200.
//   - CookieAuthMiddleware runs before PocketBase's token loader: an
//     explicit Authorization header is passed through untouched; only
//     when the header is empty does the cw_admin cookie fill it in
//     (plus e.Auth, so the middleware is correct whether it runs
//     before or after PocketBase's default token loader). Only
//     superuser tokens resolve: a non-superuser bearer gains nothing
//     here since every consuming route enforces RequireSuperuserAuth,
//     and resolving it would only turn a 401 into a 403. Tokens are
//     never logged (email= only).
package account

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/hook"
	"golang.org/x/crypto/bcrypt"

	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/limits"
	"github.com/configwire/configwire/security"
)

// dummyBcryptHash is a valid bcrypt hash of a random never-used secret.
// Unknown-email logins compare against it so they cost one bcrypt like a
// wrong-password attempt, closing the account-existence timing oracle.
const dummyBcryptHash = "$2a$10$i2Nc6VFj3nCrlIUI5bIEXOOa2rR9VP/5HqFg6JO1HPH/HuVINyZ5S"

// burnBcryptDummy spends one bcrypt on the candidate so unknown-email and
// wrong-password rejections take the same time. Result always discarded.
func burnBcryptDummy(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
}

// sessionCookieName is the HttpOnly cookie carrying the superuser token.
const sessionCookieName = "cw_admin"

// defaultSessionMaxAgeSeconds is the cookie + token lifetime (3d,
// sliding via POST /api/v1/admin/session/refresh which re-mints a fresh
// token + cookie on each call, mirroring PocketBase auth-refresh
// semantics without ever returning the token to JS). It only stops
// the browser from sending the cookie: a copied PocketBase token stays
// usable until its own collection auth-token expiry, so the cookie bound
// is not a token lifetime. The _superusers collection AuthToken
// duration is ensured to the same value (see
// ensureSuperuserTokenDuration) so the JWT outlives the cookie window
// and refresh-before-expiry keeps an active user logged in.
const defaultSessionMaxAgeSeconds = 3 * 24 * 60 * 60

// Session window bounds for CONFIGWIRE_SESSION_MAX_AGE (seconds):
// below an hour the sliding admin session is unusable (the UI refreshes
// opportunistically, not on a timer), above 30d a stolen token lives
// too long without rotation.
const (
	minSessionMaxAgeSeconds = 60 * 60
	maxSessionMaxAgeSeconds = 30 * 24 * 60 * 60
)

// sessionMaxAgeEnvName is the boot knob overriding the session window
// (cookie Max-Age and _superusers AuthToken duration together, so the
// JWT always outlives the cookie it rides in).
const sessionMaxAgeEnvName = "CONFIGWIRE_SESSION_MAX_AGE"

// sessionMaxAgeSeconds is the live cookie + token lifetime, set once
// from the env at serve startup (registerSession). Tests see the
// default (env unset); the loader is pure so env parsing is unit
// tested without mutating this.
var sessionMaxAgeSeconds = defaultSessionMaxAgeSeconds

// loadSessionMaxAgeSeconds parses CONFIGWIRE_SESSION_MAX_AGE (integer
// seconds within minSessionMaxAgeSeconds..maxSessionMaxAgeSeconds).
// Empty, malformed, or out-of-range input falls back to the default:
// silent on empty, one log line naming only the env var otherwise
// (never its value, per logging convention).
func loadSessionMaxAgeSeconds() int {
	raw := strings.TrimSpace(os.Getenv(sessionMaxAgeEnvName))
	if raw == "" {
		return defaultSessionMaxAgeSeconds
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minSessionMaxAgeSeconds || n > maxSessionMaxAgeSeconds {
		log.Printf("account: invalid %s, using default session window %d", sessionMaxAgeEnvName, defaultSessionMaxAgeSeconds)
		return defaultSessionMaxAgeSeconds
	}
	return n
}

// Exported aliases for the sso parity test (sso duplicates these to stay
// standalone). Do not introduce a second value: change these, not copies.
const SessionCookieName = sessionCookieName

// SessionMaxAgeSeconds reports the live session window in seconds.
func SessionMaxAgeSeconds() int {
	return sessionMaxAgeSeconds
}

// SessionPerIPLimit caps POST /api/v1/admin/session at 10 requests per
// SessionPerIPWindow per client IP (code constant, no knob: legit login
// is one call per 12h cookie, so 10/min/IP is far above real use and
// below password-guessing relevance). Same fixed-window shape as the
// SSO redeem bucket (sso.RedeemPerIPLimit), keyed by limits.ClientIP
// (the trusted-IP resolution the global limiter uses), 429 shape via
// apireq.TooMany (byte-identical to limits.BlockIP). Per-process memory,
// like the rest of the single-binary limiters.
const SessionPerIPLimit = 10

const SessionPerIPWindow = time.Minute

// sessionBucket is the process-wide per-IP session-login budget.
var sessionBucket = newSessionBucket()

type sessionWindow struct {
	start time.Time
	count int
}

type sessionBucketT struct {
	mu        sync.Mutex
	windows   map[string]*sessionWindow
	lastSweep time.Time
}

func newSessionBucket() *sessionBucketT {
	return &sessionBucketT{windows: make(map[string]*sessionWindow)}
}

// allowSession consumes one login token for ip, reporting false when the
// window is exhausted. Empty ip shares one "unknown" bucket instead of
// fail-opening (mirrors limits.CheckIP).
func (b *sessionBucketT) allow(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.windows[ip]
	if !ok || now.Sub(w.start) >= SessionPerIPWindow {
		w = &sessionWindow{start: now}
		b.windows[ip] = w
	}
	w.count++
	if now.Sub(b.lastSweep) >= SessionPerIPWindow {
		b.lastSweep = now
		for k, v := range b.windows {
			if now.Sub(v.start) >= 2*SessionPerIPWindow {
				delete(b.windows, k)
			}
		}
	}
	return w.count <= SessionPerIPLimit
}

// resetSessionBucket clears all per-IP buckets (test isolation).
func resetSessionBucket() {
	sessionBucket = newSessionBucket()
}

// registerSession mounts the public session endpoints. Called from Register.
func registerSession(se *core.ServeEvent) {
	sessionMaxAgeSeconds = loadSessionMaxAgeSeconds()
	se.Router.POST("/api/v1/admin/session", postSession)
	se.Router.POST("/api/v1/admin/session/refresh", refreshSession)
	se.Router.DELETE("/api/v1/admin/session", deleteSession)
	ensureSuperuserTokenDuration(se.App)
}

// ensureSuperuserTokenDuration aligns the _superusers collection
// AuthToken duration to sessionMaxAgeSeconds (default 3d, tunable via
// CONFIGWIRE_SESSION_MAX_AGE: sliding refresh re-mints a full window,
// so the JWT must outlive the cookie). Any differing value — shorter
// or longer, including operator edits via the admin UI — is reset and
// logged. Fail-soft (logs only): a stale duration just means
// shorter-lived tokens until the next serve. Runs at serve startup
// only; a live edit via the UI holds until the next restart.
func ensureSuperuserTokenDuration(app core.App) {
	col, err := app.FindCollectionByNameOrId(superusersCollection)
	if err != nil {
		return
	}
	if col.AuthToken.Duration == int64(sessionMaxAgeSeconds) {
		return
	}
	old := col.AuthToken.Duration
	col.AuthToken.Duration = int64(sessionMaxAgeSeconds)
	if err := app.Save(col); err != nil {
		log.Printf("account: failed to ensure superuser token duration: %v", err)
		return
	}
	log.Printf("account: set superuser token duration from %d to %d", old, int64(sessionMaxAgeSeconds))
}

// CookieAuthMiddleware is a global fallback that fills the Authorization
// header from the cw_admin cookie: an explicit Authorization header is
// passed through untouched for the default loader; only when the header
// is empty does the cw_admin cookie fill it in (plus e.Auth, so the
// middleware is correct whether it runs before or after PocketBase's
// default token loader). Only superuser tokens resolve: a non-superuser
// bearer gains nothing here since every consuming route enforces
// RequireSuperuserAuth, and resolving it would only turn a 401 into a
// 403. Registered with a priority just ahead of the default token loader
// so the header is in place for every downstream consumer.
func CookieAuthMiddleware() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       "cwAdminCookieAuth",
		Priority: apis.DefaultLoadAuthTokenMiddlewarePriority - 1,
		Func: func(re *core.RequestEvent) error {
			if header := strings.TrimSpace(re.Request.Header.Get("Authorization")); header != "" {
				return re.Next()
			}
			if c, err := re.Request.Cookie(sessionCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
				token := strings.TrimSpace(c.Value)
				re.Request.Header.Set("Authorization", token)
				if re.Auth == nil {
					if rec, err := re.App.FindAuthRecordByToken(token, core.TokenTypeAuth); err == nil && rec != nil && rec.IsSuperuser() {
						re.Auth = rec
					}
				}
			}
			return re.Next()
		},
	}
}

type sessionRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// sessionOriginDenied is the POST CSRF guard: a present Origin/Referer
// must be same-origin (host, including port, equals the request Host),
// else the request is rejected. Absent headers (curl, same-origin
// navigations without Origin) pass.
func sessionOriginDenied(re *core.RequestEvent) bool {
	for _, h := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(re.Request.Header.Get(h))
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return true
		}
		if !strings.EqualFold(u.Host, re.Request.Host) {
			return true
		}
	}
	return false
}

// findSuperuserByEmail returns the _superusers record with a
// case-insensitive email match, or nil when none exists. The table
// holds a handful of rows, so a scan matches countRealSuperusers.
func findSuperuserByEmail(app core.App, email string) *core.Record {
	recs, err := app.FindAllRecords(superusersCollection)
	if err != nil {
		return nil
	}
	for _, r := range recs {
		if strings.EqualFold(r.GetString("email"), email) {
			return r
		}
	}
	return nil
}

// sessionSecure reports whether the cookie must carry the Secure flag:
// direct TLS, or a proxy-terminated https session signalled by
// X-Forwarded-Proto: https from a trusted proxy peer only (same trust
// boundary as limits.ClientIP). An untrusted peer's header is ignored,
// so a client cannot influence another browser's cookie.
func sessionSecure(re *core.RequestEvent) bool {
	if re.IsTLS() {
		return true
	}
	if !limits.IsTrustedProxyPeer(re.Request.RemoteAddr) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(re.Request.Header.Get("X-Forwarded-Proto")), "https")
}

// setSessionCookie stamps the cw_admin cookie. Secure follows TLS or
// X-Forwarded-Proto so proxies terminating TLS still get it, while
// plain-HTTP local dev keeps working.
func setSessionCookie(re *core.RequestEvent, token string) {
	secure := sessionSecure(re)
	http.SetCookie(re.Response, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   sessionMaxAgeSeconds,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearSessionCookie expires the cw_admin cookie (logout). It emits both
// the Secure and non-Secure clearing variants so logout deletes the
// cookie even when the scheme differs from login (dev port-forward,
// health-check path, proxy misconfig): a Secure cookie is only removed
// by a Secure clearing Set-Cookie and vice versa.
func clearSessionCookie(re *core.RequestEvent) {
	for _, secure := range []bool{true, false} {
		http.SetCookie(re.Response, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Expires:  time.Unix(1, 0).UTC(),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

// postSession handles POST /api/v1/admin/session (PUBLIC).
// Order: 429 (per-IP budget, before any bcrypt) -> 403 (Origin present
// but not same-origin) -> 400 (validation, including the legacy {token}
// shape) -> 401 (bad credentials) -> 200 {"ok":true} with the cw_admin
// cookie set. The token never appears in the body or the logs (email= only).
func postSession(re *core.RequestEvent) error {
	security.SetHeaders(re)
	// Cheap reject before bcrypt/validation: per-IP fixed window.
	if !sessionBucket.allow(limits.ClientIP(re)) {
		return apireq.TooMany(re, "Rate limit exceeded.")
	}
	if sessionOriginDenied(re) {
		return re.JSON(http.StatusForbidden, map[string]any{
			"message": "cross-origin session requests are not allowed.",
			"status":  http.StatusForbidden,
		})
	}
	var req struct {
		sessionRequest
		Token *string `json:"token"`
	}
	if err := decodeBody(re, &req); err != nil {
		return err
	}
	// The migration-only {token} adoption is removed: a legacy body
	// carrying a token is rejected so holders without a cookie re-login
	// with email+password. Probed on the same decode (BindBody consumes
	// the body, so a second decode would misread it as empty).
	if req.Token != nil && strings.TrimSpace(*req.Token) != "" {
		return re.BadRequestError("token login is no longer supported: sign in with email and password.", nil)
	}
	if err := ValidateCredentials(req.Email, req.Password); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	email := strings.TrimSpace(req.Email)
	rec := findSuperuserByEmail(re.App, email)
	if rec == nil {
		burnBcryptDummy(req.Password)
		log.Printf("account: session login failed email=%s", email)
		return re.UnauthorizedError("invalid email or password.", nil)
	}
	if !rec.ValidatePassword(req.Password) {
		log.Printf("account: session login failed email=%s", email)
		return re.UnauthorizedError("invalid email or password.", nil)
	}
	token, err := rec.NewAuthToken()
	if err != nil {
		return internalError(re, "failed to create session", err)
	}
	log.Printf("account: session created email=%s", email)
	setSessionCookie(re, token)
	return re.JSON(http.StatusOK, map[string]any{"ok": true})
}

// refreshSession handles POST /api/v1/admin/session/refresh
// (cookie-authed): validates the current cookie bearer, mints a fresh
// token via NewAuthToken, and re-sets the cw_admin cookie, returning
// {"ok":true} with no token in the body. This is the cookie-only
// equivalent of PocketBase's auth-refresh (same sliding semantics:
// each call extends the JWT + cookie by a full window), without exposing
// the native auth-refresh JSON token to JS. Cookie-only by design: the
// Authorization header is ignored here so a bearer exfiltrated for ops
// tooling cannot be kept alive without the cookie. Order: 403 (Origin
// present but not same-origin) -> 401 (missing/invalid/non-superuser
// bearer) -> 200 {"ok":true} with the rotated cookie.
func refreshSession(re *core.RequestEvent) error {
	security.SetHeaders(re)
	if sessionOriginDenied(re) {
		return re.JSON(http.StatusForbidden, map[string]any{
			"message": "cross-origin session requests are not allowed.",
			"status":  http.StatusForbidden,
		})
	}
	c, err := re.Request.Cookie(sessionCookieName)
	token := ""
	if err == nil && c != nil {
		token = strings.TrimSpace(c.Value)
	}
	if token == "" {
		return re.UnauthorizedError("missing session.", nil)
	}
	rec, err := re.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
	if err != nil || rec == nil || !rec.IsSuperuser() {
		return re.UnauthorizedError("invalid session.", nil)
	}
	fresh, err := rec.NewAuthToken()
	if err != nil {
		return internalError(re, "failed to refresh session", err)
	}
	setSessionCookie(re, fresh)
	return re.JSON(http.StatusOK, map[string]any{"ok": true})
}

// deleteSession handles DELETE /api/v1/admin/session: clears the
// cw_admin cookie and returns {"ok":true}. Idempotent by design (empty
// logout is 200), so no auth and no Origin check (SameSite=Strict
// already scopes the cookie). Always 200: the browser copy is gone and
// a copied token stays valid until its own collection auth-token
// expiry by design.
func deleteSession(re *core.RequestEvent) error {
	security.SetHeaders(re)
	clearSessionCookie(re)
	return re.JSON(http.StatusOK, map[string]any{"ok": true})
}
