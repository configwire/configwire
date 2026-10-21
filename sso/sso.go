// Package sso implements the tenant side of the ConfigWire Cloud SSO
// ticket bridge (plan cloud-sso-bridge P1, normative contract
// docs/sso-ticket.md + docs/sso_vectors.json — conform exactly).
//
// Tenant surface:
//   - POST /api/v1/admin/sso/redeem {ticket} (public): validate the
//     HMAC ticket per spec §5, enforce nonce single-use, and return
//     200 {token} via PocketBase record.NewAuthToken() semantics for the
//     matched non-installer _superusers record.
//   - GET /api/v1/admin/auth/mode -> {mode} (public): the UI mode signal
//     ("standard" default, "sso" when cloud-managed).
//
// Rate limiting: redeem reuses the same protection as the public setup
// endpoint (both live under /api/v1/admin/ and are gated by the global
// limits.CheckIP middleware: admin-IP allowlist) PLUS a small per-route
// per-IP bucket below (RedeemPerIPLimit per RedeemPerIPWindow). Legit
// redeem is one call per login, so 10/min/IP is far above real use and
// below brute-force relevance. The bucket key is the client IP via
// limits.ClientIP (the same trusted-IP resolution the global limiter
// uses), the 429 shape reuses apireq.TooMany ("Rate limit exceeded." +
// Retry-After: 1, byte-identical to limits.BlockIP), and the bucket is
// per-process memory like the nonce cache above — consistent with the
// documented single-binary model (cf. production-hardening plan §7
// risk 3: a NAT-shared dashboard brushing 10/min/IP is a false
// positive; the escape hatch is bumping the constant, not a new env
// knob).
//
// Security: the ticket, secret, MAC, and nonce NEVER appear in logs —
// log lines carry email= only (existing account.go convention).
package sso

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/limits"
	"github.com/configwire/configwire/security"

	"github.com/pocketbase/pocketbase/core"
)

// Auth modes for CONFIGWIRE_AUTH_MODE (default ModeStandard).
const (
	// ModeStandard is today's UI + API, untouched.
	ModeStandard = "standard"
	// ModeSSO hides the native login UI and enables ticket redeem.
	ModeSSO = "sso"
)

// Env knobs (CONFIGWIRE_* convention, cf. main.go).
const (
	// EnvAuthMode selects ModeStandard (default) or ModeSSO.
	EnvAuthMode = "CONFIGWIRE_AUTH_MODE"
	// EnvSecret carries the 32 secret bytes hex-encoded (64 chars),
	// injected by the orchestrator compose fragment (P2).
	EnvSecret = "CONFIGWIRE_CLOUD_SSO_SECRET"
	// EnvCloudURL optionally carries the cloud dashboard URL for the
	// managed-by-cloud notice link (unset = notice without a link).
	EnvCloudURL = "CONFIGWIRE_CLOUD_URL"
)

// NonceTTL is the ticket lifetime (60s, non-negotiable per spec §4) and
// the nonce-cache entry TTL.
const NonceTTL = 60 * time.Second

// Wire bodies (spec §6, exact lowercase). Malformed and tampered share
// one body so the response never says which check failed; only the
// expired variant is distinguished (same 400 status).
const (
	msgMalformedOrTampered = "invalid ticket: malformed or tampered."
	msgExpired             = "invalid ticket: expired."
	msgUnknownUser         = "unknown user."
	msgReplay              = "ticket already redeemed."
	msgSSODisabled         = "sso not enabled."
)

// Validation outcomes (first failure wins per spec §5).
var (
	// ErrTicketMalformed covers shape/base64/expiry/hex/email failures.
	ErrTicketMalformed = errors.New(msgMalformedOrTampered)
	// ErrTicketExpired covers past-expiry tickets (same 400 status).
	ErrTicketExpired = errors.New(msgExpired)
	// ErrTicketTampered covers HMAC mismatch (same body as malformed).
	ErrTicketTampered = errors.New(msgMalformedOrTampered)
)

// ParseAuthMode normalizes the CONFIGWIRE_AUTH_MODE value: "sso"
// (case-insensitive, surrounding whitespace ignored) selects SSO mode;
// anything else — including empty (unset) — is standard.
func ParseAuthMode(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), ModeSSO) {
		return ModeSSO
	}
	return ModeStandard
}

// ParseSecretHex decodes the CONFIGWIRE_CLOUD_SSO_SECRET value: exactly
// 64 hex chars decoding to 32 bytes. Hex casing is accepted leniently;
// length and charset are strict. Missing/undersized/non-hex values
// error so sso mode can fail fast at startup with the message.
func ParseSecretHex(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if len(s) != 64 {
		return nil, errors.New("must be 64 hex chars (32 bytes), got " + strconv.Itoa(len(s)) + " chars")
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return nil, errors.New("must be 64 hex chars (32 bytes): invalid hex")
	}
	return b, nil
}

// ParsedTicket is a validated ticket's claims.
type ParsedTicket struct {
	// Email is the trimmed owner email (exact match against the
	// _superusers record).
	Email string
	// Expiry is the ticket's Unix-seconds expiry (UTC).
	Expiry int64
	// Nonce is the 32-char lowercase-hex single-use nonce.
	Nonce string
}

// ValidateTicket validates ticket purely (no I/O) in the exact spec §5
// order: shape -> email -> expiry -> time -> nonce/mac shape -> HMAC.
// Replay (cache) and owner lookup are the handler's job, after this.
// secret is the 32 raw secret bytes; nowUnix is wall-clock UTC seconds.
func ValidateTicket(ticket string, secret []byte, nowUnix int64) (*ParsedTicket, error) {
	// 1. Shape: exactly 4 non-empty dot segments.
	parts := strings.Split(ticket, ".")
	if len(parts) != 4 {
		return nil, ErrTicketMalformed
	}
	for _, p := range parts {
		if p == "" {
			return nil, ErrTicketMalformed
		}
	}
	emailSeg, expirySeg, nonceSeg, macSeg := parts[0], parts[1], parts[2], parts[3]

	// 2. Decode email segment (base64url no-pad) + canonical re-encode
	// + email shape (same validateEmail rules as
	// cloud/orchestrator/validate.go).
	emailRaw, err := base64.RawURLEncoding.DecodeString(emailSeg)
	if err != nil {
		return nil, ErrTicketMalformed
	}
	if base64.RawURLEncoding.EncodeToString(emailRaw) != emailSeg {
		return nil, ErrTicketMalformed
	}
	email := strings.TrimSpace(string(emailRaw))
	if err := validateTicketEmail(email); err != nil {
		return nil, ErrTicketMalformed
	}

	// 3. Parse expiry integer (ASCII decimal, 1-10 digits, no sign).
	if strings.HasPrefix(expirySeg, "+") || strings.HasPrefix(expirySeg, "-") {
		return nil, ErrTicketMalformed
	}
	if len(expirySeg) > 10 {
		return nil, ErrTicketMalformed
	}
	expiry, err := strconv.ParseInt(expirySeg, 10, 64)
	if err != nil {
		return nil, ErrTicketMalformed
	}

	// 4. Time: valid on [mint, mint+60]; accept iff now <= expiry.
	if nowUnix > expiry {
		return nil, ErrTicketExpired
	}

	// 5. Nonce (32 lowercase hex) + MAC (64 lowercase hex) shape.
	// Explicit charsets: hex.DecodeString would accept uppercase.
	if !isLowerHex(nonceSeg, 32) || !isLowerHex(macSeg, 64) {
		return nil, ErrTicketMalformed
	}

	// 6. Recompute HMAC, constant-time compare (never ==).
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(email + "\n" + expirySeg + "\n" + nonceSeg))
	got, _ := hex.DecodeString(macSeg)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return nil, ErrTicketTampered
	}

	return &ParsedTicket{Email: email, Expiry: expiry, Nonce: nonceSeg}, nil
}

// isLowerHex reports whether s is exactly n lowercase-hex chars.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// validateTicketEmail enforces the same rules as
// cloud/orchestrator/validate.go: trimmed non-empty, max 255 chars,
// exactly one @ with non-empty local/domain parts and a dot in the
// domain, no whitespace, installer address reserved. The input is
// already trimmed; failures read as malformed (the caller maps them).
func validateTicketEmail(email string) error {
	if email == "" {
		return errors.New("email must not be empty.")
	}
	if len(email) > 255 {
		return errors.New("email must be at most 255 characters.")
	}
	if strings.Count(email, "@") != 1 {
		return errors.New("email must contain exactly one @.")
	}
	if strings.ContainsAny(email, " \t\n\r") {
		return errors.New("email must not contain whitespace.")
	}
	local, domain, _ := strings.Cut(email, "@")
	if local == "" || domain == "" {
		return errors.New("email must have non-empty local and domain parts.")
	}
	if !strings.Contains(domain, ".") {
		return errors.New("email domain must contain a dot.")
	}
	if strings.EqualFold(email, core.DefaultInstallerEmail) {
		return errors.New("email is reserved for the system installer.")
	}
	return nil
}

// MintTicket builds a ticket string (stdlib only, per spec §9): the
// orchestrator mint path (P2) and tests use this shape.
func MintTicket(email string, expiry int64, nonce string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(email + "\n" + strconv.FormatInt(expiry, 10) + "\n" + nonce))
	return base64.RawURLEncoding.EncodeToString([]byte(email)) +
		"." + strconv.FormatInt(expiry, 10) + "." + nonce + "." + hex.EncodeToString(mac.Sum(nil))
}

// GenerateNonce returns 16 crypto/rand bytes as 32 lowercase hex chars.
func GenerateNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// NonceCache is the in-memory single-use nonce store (entry TTL = ticket
// TTL, 60s; opportunistic sweep mirrors the ratelimit idle sweep).
//
// Per-process caveat (documented, accepted per spec §5): with N tenant
// processes, a replay hitting a different process within the 60s window
// can pass — the same trade-off as the existing per-process rate-limit
// caveat. Replay window is <= 60s worst case.
type NonceCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	seen      map[string]time.Time
	lastSweep time.Time
}

// NewNonceCache builds a cache with entry TTL ttl (production: NonceTTL).
func NewNonceCache(ttl time.Duration) *NonceCache {
	return &NonceCache{ttl: ttl, seen: make(map[string]time.Time)}
}

// Claim atomically checks and inserts nonce: true means the nonce was
// already redeemed (409 replay); false means it is now claimed. The
// handler calls Claim BEFORE issuing the token (insert-then-issue),
// which closes the double-redeem race for concurrent redeems of one
// ticket within this process.
func (c *NonceCache) Claim(nonce string) bool {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.lastSweep) >= time.Second {
		c.lastSweep = now
		for k, t := range c.seen {
			if now.Sub(t) >= c.ttl {
				delete(c.seen, k)
			}
		}
	}
	if t, ok := c.seen[nonce]; ok && now.Sub(t) < c.ttl {
		return true
	}
	c.seen[nonce] = now
	return false
}

// RedeemPerIPLimit caps POST /api/v1/admin/sso/redeem at 10 requests
// per RedeemPerIPWindow per client IP. Code constant by decision (no
// knob sprawl): legit redeem is one call per login, so 10/min/IP is far
// above real use — if a NAT-shared dashboard ever brushes it (plan §7
// risk 3), bump this constant.
const RedeemPerIPLimit = 10

const RedeemPerIPWindow = time.Minute

// redeemBucket is a minimal per-IP fixed-window counter (stdlib only).
// limits.Limiter is not reused: its window is hardcoded to 1s with no
// per-route budget, while redeem needs a 1-minute window — importing
// limits only for ClientIP keeps the trusted-IP resolution identical to
// the global limiter without duplicating it. Per-process memory like
// NonceCache and limits (single-binary model); opportunistic sweeps
// (<=1/window) drop entries idle for >2 windows.
type redeemBucket struct {
	mu        sync.Mutex
	windows   map[string]*redeemWindow
	lastSweep time.Time
}

type redeemWindow struct {
	start time.Time
	count int
}

func newRedeemBucket() *redeemBucket {
	return &redeemBucket{windows: make(map[string]*redeemWindow)}
}

// allow consumes one token for ip, reporting false when the window is
// exhausted. Empty ip shares one "unknown" bucket instead of
// fail-opening (mirrors limits.CheckIP).
func (b *redeemBucket) allow(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.windows[ip]
	if !ok || now.Sub(w.start) >= RedeemPerIPWindow {
		w = &redeemWindow{start: now}
		b.windows[ip] = w
	}
	w.count++
	if now.Sub(b.lastSweep) >= RedeemPerIPWindow {
		b.lastSweep = now
		for k, v := range b.windows {
			if now.Sub(v.start) >= 2*RedeemPerIPWindow {
				delete(b.windows, k)
			}
		}
	}
	return w.count <= RedeemPerIPLimit
}

// superusersCollection is the PocketBase auth collection holding admins.
const superusersCollection = core.CollectionNameSuperusers

// Config wires the SSO routes.
type Config struct {
	// Mode is ModeStandard or ModeSSO (ParseAuthMode).
	Mode string
	// Secret is the 32 raw secret bytes; empty in standard mode, where
	// redeem answers 404 (sso not enabled) without touching auth.
	Secret []byte
	// Cache is the process-wide nonce cache; nil builds a default one.
	Cache *NonceCache
	// CloudURL optionally surfaces in the mode response for the
	// managed-by-cloud notice link (empty = notice without a link).
	CloudURL string
}

type server struct {
	mode     string
	secret   []byte
	cache    *NonceCache
	cloudURL string

	mu     sync.Mutex
	redeem *redeemBucket
}

// allowRedeem consumes one per-IP redeem token, lazily creating the
// bucket so zero-value servers (incl. older test literals) keep working.
func (s *server) allowRedeem(ip string) bool {
	s.mu.Lock()
	if s.redeem == nil {
		s.redeem = newRedeemBucket()
	}
	b := s.redeem
	s.mu.Unlock()
	return b.allow(ip)
}

// Register mounts the public SSO routes (no auth middleware: redeem
// carries its own HMAC auth, mode is a pre-auth UI signal).
func Register(se *core.ServeEvent, cfg Config) {
	mode := cfg.Mode
	if mode != ModeSSO {
		mode = ModeStandard
	}
	cache := cfg.Cache
	if cache == nil {
		cache = NewNonceCache(NonceTTL)
	}
	s := &server{mode: mode, secret: cfg.Secret, cache: cache, cloudURL: strings.TrimSpace(cfg.CloudURL), redeem: newRedeemBucket()}
	se.Router.GET("/api/v1/admin/auth/mode", s.getMode)
	se.Router.POST("/api/v1/admin/sso/redeem", s.postRedeem)
}

// getMode handles GET /api/v1/admin/auth/mode (PUBLIC).
// 200 {"mode": "standard"|"sso"} (+ "cloudUrl" only when configured).
func (s *server) getMode(re *core.RequestEvent) error {
	security.SetHeaders(re)
	body := map[string]any{"mode": s.mode}
	if s.cloudURL != "" {
		body["cloudUrl"] = s.cloudURL
	}
	return re.JSON(http.StatusOK, body)
}

type redeemRequest struct {
	Ticket string `json:"ticket"`
}

// postRedeem handles POST /api/v1/admin/sso/redeem (PUBLIC).
// Order: 404 (sso not enabled in standard mode) -> 400 (body/
// malformed/expired/tampered) -> 409 (replay) -> 404 (unknown user) ->
// 200 {token}. Bodies are written via re.JSON (not ApiError) so the
// spec §6 lowercase shapes go out verbatim — the router sentenizes
// ApiError messages. Never logs ticket/secret/nonce/MAC (email= only).
func (s *server) postRedeem(re *core.RequestEvent) error {
	security.SetHeaders(re)
	if len(s.secret) == 0 {
		return re.JSON(http.StatusNotFound, map[string]any{
			"message": msgSSODisabled,
			"status":  http.StatusNotFound,
		})
	}
	// Cheap reject before any HMAC work: per-IP bucket keyed by the same
	// trusted-IP resolution the global limiter uses. 429 shape reuses
	// apireq.TooMany so it is byte-identical to limits.BlockIP.
	if !s.allowRedeem(limits.ClientIP(re)) {
		return apireq.TooMany(re, "Rate limit exceeded.")
	}
	var req redeemRequest
	if err := apireq.DecodeBody(re, &req); err != nil {
		return err
	}
	pt, err := ValidateTicket(req.Ticket, s.secret, time.Now().Unix())
	switch {
	case errors.Is(err, ErrTicketExpired):
		return ticketError(re, http.StatusBadRequest, msgExpired)
	case err != nil:
		// Malformed and tampered share one body (no oracle).
		return ticketError(re, http.StatusBadRequest, msgMalformedOrTampered)
	}
	// Insert-then-issue: claim the nonce BEFORE the token exists.
	if s.cache.Claim(pt.Nonce) {
		log.Print("sso: redeem rejected (replay)")
		return ticketError(re, http.StatusConflict, msgReplay)
	}
	rec, err := re.App.FindFirstRecordByData(superusersCollection, "email", pt.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("sso: redeem for unknown user email=%s", pt.Email)
			return ticketError(re, http.StatusNotFound, msgUnknownUser)
		}
		return apireq.Internal(re, "sso: failed to look up superuser", err)
	}
	// Exact match after TrimSpace (validation already trimmed); the
	// installer placeholder is never a redeem target.
	if rec == nil || rec.GetString("email") != pt.Email ||
		strings.EqualFold(pt.Email, core.DefaultInstallerEmail) {
		log.Printf("sso: redeem for unknown user email=%s", pt.Email)
		return ticketError(re, http.StatusNotFound, msgUnknownUser)
	}
	token, err := rec.NewAuthToken()
	if err != nil {
		return apireq.Internal(re, "sso: failed to issue token", err)
	}
	log.Printf("sso: redeemed ticket email=%s", pt.Email)
	return re.JSON(http.StatusOK, map[string]any{"token": token})
}

// ticketError renders a spec-exact error body ({message, status}).
func ticketError(re *core.RequestEvent, status int, msg string) error {
	return re.JSON(status, map[string]any{"message": msg, "status": status})
}
