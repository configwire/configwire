// Cookie-carried superuser auth: the Admin UI no longer persists the
// superuser token in localStorage. Instead the browser holds it in a
// JS-invisible cookie and the middleware below re-attaches it per
// request. This is NOT a server-side session: nothing is stored
// server-side, so clearing the cookie only removes the token from that
// browser — the underlying PocketBase token stays valid until its own
// collection auth-token expiry. Revocation means rotating/ending the
// token itself, not deleting the cookie.
//
//   - POST /api/v1/admin/session (PUBLIC, Origin-checked, per-IP
//     rate-limited) accepts {email,password} or {token}. On success it
//     sets the cw_admin cookie (HttpOnly, SameSite=Strict, Path=/,
//     Max-Age 12h; Secure when the request is TLS or the proxy signals
//     https via X-Forwarded-Proto, so plain-HTTP local dev keeps
//     working) and returns {"ok":true} with no token in the body.
//     Direct auth-with-password login is untouched.
//   - DELETE /api/v1/admin/session clears the cookie, returns
//     {"ok":true}. Idempotent; no auth required (logout).
//   - CookieAuthMiddleware runs before PocketBase's token loader: when
//     the Authorization header is empty and the cw_admin cookie is
//     present it copies the cookie value into the header (and resolves
//     e.Auth directly so ordering against the default loader cannot
//     matter). An explicit Authorization header always wins; only
//     superuser tokens resolve (route-layer RequireSuperuserAuth still
//     enforces), so a non-superuser bearer can never gain admin rights
//     through the cookie path. Nothing is stored
//     server-side and the token value is never logged (email= only).
package account

import (
	"log"
	"net/http"
	"net/url"
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

// sessionMaxAgeSeconds is the cookie lifetime (12h). It only stops
// the browser from sending the cookie: a copied PocketBase token stays
// usable until its own collection auth-token expiry, so the cookie bound
// is not a token lifetime.
const sessionMaxAgeSeconds = 12 * 60 * 60

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
	se.Router.POST("/api/v1/admin/session", postSession)
	se.Router.DELETE("/api/v1/admin/session", deleteSession)
}

// CookieAuthMiddleware is a global fallback: explicit Authorization
// headers keep working unchanged; only when the header is empty does
// the cw_admin cookie fill it in (plus e.Auth, so the middleware is
// correct whether it runs before or after PocketBase's default token
// loader). Only superuser tokens resolve: a non-superuser bearer gains
// nothing here since every consuming route enforces
// RequireSuperuserAuth, and resolving it would only turn a 401 into a
// 403. Registered with a priority just ahead of the default token
// loader so the header is in place for every downstream consumer.
func CookieAuthMiddleware() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       "cwAdminCookieAuth",
		Priority: apis.DefaultLoadAuthTokenMiddlewarePriority - 1,
		Func: func(re *core.RequestEvent) error {
			if re.Request.Header.Get("Authorization") == "" {
				if c, err := re.Request.Cookie(sessionCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
					token := strings.TrimSpace(c.Value)
					re.Request.Header.Set("Authorization", token)
					if re.Auth == nil {
						if rec, err := re.App.FindAuthRecordByToken(token, core.TokenTypeAuth); err == nil && rec != nil && rec.IsSuperuser() {
							re.Auth = rec
						}
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
	Token    string `json:"token"`
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
// but not same-origin) -> 400 (validation) -> 401 (bad
// credentials/token) -> 200 {"ok":true} with the cw_admin cookie set.
// The token never appears in the body or the logs (email= only).
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
	var req sessionRequest
	if err := decodeBody(re, &req); err != nil {
		return err
	}
	var token string
	if strings.TrimSpace(req.Token) != "" {
		// Token mode (MIGRATION ONLY, do not extend): adopt an existing
		// superuser token so pre-cookie localStorage holders migrate to
		// the cookie without re-entering the password. The token is
		// validated, never echoed back. Any bearer that reaches this
		// endpoint upgrades into the persistent cookie, so this path
		// exists only for the migration window and should be removed
		// once stored tokens have rolled (the password path below is
		// the steady-state login).
		token = strings.TrimSpace(req.Token)
		rec, err := re.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
		if err != nil || rec == nil || !rec.IsSuperuser() {
			return re.UnauthorizedError("invalid token.", nil)
		}
		log.Printf("account: session created via token email=%s", rec.GetString("email"))
	} else {
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
		t, err := rec.NewAuthToken()
		if err != nil {
			return internalError(re, "failed to create session", err)
		}
		token = t
		log.Printf("account: session created email=%s", email)
	}
	setSessionCookie(re, token)
	return re.JSON(http.StatusOK, map[string]any{"ok": true})
}

// deleteSession handles DELETE /api/v1/admin/session: clears the
// cw_admin cookie and returns {"ok":true}. Idempotent by design, so no
// auth and no Origin check (SameSite=Strict already scopes the cookie).
func deleteSession(re *core.RequestEvent) error {
	security.SetHeaders(re)
	clearSessionCookie(re)
	return re.JSON(http.StatusOK, map[string]any{"ok": true})
}
