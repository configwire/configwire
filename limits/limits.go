// Package limits provides tunable per-second request protection, including
// the per-key gate for SDK traffic: fetch/ingest handlers call
// AllowFetchKey / AllowIngestKey with EffectiveFetchRps /
// EffectiveIngestRps (per-key sdk_keys.fetchRps/ingestRps overrides over
// global defaults) on separate fetch:/ingest: namespaces.
//
// Why this exists: the ingest/fetch Allow checks run AFTER RequireSDKKey,
// so an unauthenticated flood on /api/* bypasses them entirely. The IP
// limiter here is designed to run BEFORE auth (the wiring layer places
// CheckIP/Middleware first) — see CheckIP.
//
// Model: per-IP and per-key fixed windows of 1 second (map+mutex+idle
// sweep, mirroring ingest.Limiter). Tunables live in Config, defaulted
// from code, overridden by env, overridden at runtime by the
// rate_settings collection row (key=global) via the admin API.
package limits

import (
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/configwire/configwire/security"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Defaults allow the k6 load gate (100 rps fetch + 50 rps exposure)
// through a single IP: GlobalRps=200 headroom above 150.
const (
	DefaultGlobalRps = 200
	DefaultBurst     = 400
	DefaultFetchRps  = 100
	DefaultIngestRps = 50
	DefaultAdminRps  = 20

	// Window is the fixed rate-limit window for every limiter here.
	Window = time.Second
	// WindowSec is the wire representation of Window for admin responses.
	WindowSec = 1

	// MinRps / MaxRps bound every tunable (admin PUT validates 1..10000).
	MinRps = 1
	MaxRps = 10000
)

// Config carries the tunable per-second budgets.
type Config struct {
	GlobalRps int `json:"globalRps"`
	// Burst is a hard ceiling / short-burst cap, NOT a token-bucket burst
	// above the sustained rate: effective limit = min(GlobalRps, Burst).
	Burst     int `json:"burst"`
	FetchRps  int `json:"fetchRps"`
	IngestRps int `json:"ingestRps"`
	AdminRps  int `json:"adminRps"`
}

// Defaults returns the code defaults (before env/DB overrides).
func Defaults() Config {
	return Config{
		GlobalRps: DefaultGlobalRps,
		Burst:     DefaultBurst,
		FetchRps:  DefaultFetchRps,
		IngestRps: DefaultIngestRps,
		AdminRps:  DefaultAdminRps,
	}
}

// parseEnvInt reads an int env var, falling back to def on empty/invalid.
func parseEnvInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// loadDefaultsFromEnv applies the CONFIGWIRE_* overrides over Defaults.
// Invalid (non-numeric) values fall back to the default; out-of-range env
// values are clamped by clampConfig at package init and again in
// ensureLoaded on the serve path.
func loadDefaultsFromEnv() Config {
	c := Defaults()
	c.GlobalRps = parseEnvInt("CONFIGWIRE_GLOBAL_RPS", c.GlobalRps)
	c.Burst = parseEnvInt("CONFIGWIRE_BURST", c.Burst)
	c.FetchRps = parseEnvInt("CONFIGWIRE_FETCH_RPS", c.FetchRps)
	c.IngestRps = parseEnvInt("CONFIGWIRE_INGEST_RPS", c.IngestRps)
	c.AdminRps = parseEnvInt("CONFIGWIRE_ADMIN_RPS", c.AdminRps)
	return c
}

// ValidateConfig enforces 1..10000 on every tunable, checked in struct
// order so multi-field failures always report the same field first.
func ValidateConfig(c Config) error {
	fields := []struct {
		name string
		v    int
	}{
		{"globalRps", c.GlobalRps},
		{"burst", c.Burst},
		{"fetchRps", c.FetchRps},
		{"ingestRps", c.IngestRps},
		{"adminRps", c.AdminRps},
	}
	for _, f := range fields {
		if f.v < MinRps || f.v > MaxRps {
			return &configError{msg: "invalid " + f.name + ": must be 1..10000."}
		}
	}
	return nil
}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

// clampConfig forces every field into 1..10000 (used when loading env/DB
// values that bypass admin validation).
func clampConfig(c Config) Config {
	clamp := func(v, def int) int {
		if v < MinRps || v > MaxRps {
			return def
		}
		return v
	}
	d := Defaults()
	c.GlobalRps = clamp(c.GlobalRps, d.GlobalRps)
	c.Burst = clamp(c.Burst, d.Burst)
	c.FetchRps = clamp(c.FetchRps, d.FetchRps)
	c.IngestRps = clamp(c.IngestRps, d.IngestRps)
	c.AdminRps = clamp(c.AdminRps, d.AdminRps)
	return c
}

// Limiter is a per-id fixed-window rate limiter over Window (1s).
// Memory-growth note (same trade-off as ingest.Limiter): one entry per
// distinct id ever seen; opportunistic sweeps (≤1/s) delete entries idle
// for >2 windows, so steady-state size ≈ active IPs/keys. IPs ARE
// attacker-influenced (X-Forwarded-For is spoofable), but the 1s window
// with 2-window expiry bounds each entry's lifetime to ~2s, so a scan
// attack only grows the map while the flood is ongoing.
type Limiter struct {
	mu        sync.Mutex
	windows   map[string]*rateWindow
	window    time.Duration
	lastSweep time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

// NewLimiter builds a Limiter over the 1s Window.
func NewLimiter() *Limiter {
	return &Limiter{windows: make(map[string]*rateWindow), window: Window}
}

// newLimiterWithWindow is the test seam for window behavior.
func newLimiterWithWindow(d time.Duration) *Limiter {
	return &Limiter{windows: make(map[string]*rateWindow), window: d}
}

// Allow consumes one token for id, reporting false when the window is exhausted.
// A non-positive limit falls back to DefaultGlobalRps without logging:
// this sits on the per-request hot path, so per-hit log.Printf would let
// a misconfigured caller flood the server log.
func (l *Limiter) Allow(id string, limit int) bool {
	if limit <= 0 {
		limit = DefaultGlobalRps
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[id]
	if !ok || now.Sub(w.start) >= l.window {
		w = &rateWindow{start: now}
		l.windows[id] = w
	}
	w.count++
	if now.Sub(l.lastSweep) >= l.window {
		l.lastSweep = now
		for k, v := range l.windows {
			if now.Sub(v.start) >= 2*l.window {
				delete(l.windows, k)
			}
		}
	}
	return w.count <= limit
}

// Size reports tracked id count for tests/QA only.
func (l *Limiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.windows)
}

// Module state: atomic config under RWMutex plus two dedicated limiters
// (ip, admin) and one shared per-key limiter whose ids are namespaced per
// traffic class ("fetch:"+hash / "ingest:"+hash), so fetch/ingest budgets
// stay logically independent without consuming each other.
var (
	cfgMu        sync.RWMutex
	current      = clampConfig(loadDefaultsFromEnv())
	ipLimiter    = NewLimiter()
	keyLimiter   = NewLimiter()
	adminLimiter = NewLimiter()
)

// resetForTest replaces config + limiters (tests only, same package).
func resetForTest(c Config) {
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
	ipLimiter = NewLimiter()
	keyLimiter = NewLimiter()
	adminLimiter = NewLimiter()
}

// GetConfig returns a snapshot of the active config.
func GetConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return current
}

// applyConfig validates and installs c (admin PUT path).
func applyConfig(c Config) error {
	if err := ValidateConfig(c); err != nil {
		return err
	}
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
	return nil
}

// AllowIP consumes one global-budget token for ip. Effective limit =
// min(GlobalRps, Burst) — Burst is a hard ceiling / short-burst cap, NOT
// a token-bucket burst above the sustained rate. With defaults 200/400
// the effective rate is 200 rps per IP per second.
func AllowIP(ip string) bool {
	c := GetConfig()
	limit := c.GlobalRps
	if c.Burst < limit {
		limit = c.Burst
	}
	return ipLimiter.Allow(ip, limit)
}

// AllowKey consumes one per-second token for an SDK key hash at an
// explicit limit. Generic primitive only: fetch/ingest paths must use the
// namespaced AllowFetch/AllowIngest above, never this with a bare hash,
// or their budgets would share one bucket.
func AllowKey(hash string, limit int) bool {
	return keyLimiter.Allow(hash, limit)
}

// AllowFetch consumes one per-second token for a fetch key hash on its
// own namespaced per-key bucket ("fetch:"+hash), independent of ingest.
func AllowFetch(keyHash string) bool {
	return AllowFetchKey(keyHash, GetConfig().FetchRps)
}

// AllowFetchKey consumes one per-second token for a fetch key hash at an
// explicit limit (per-key override path) on the fetch namespace.
func AllowFetchKey(keyHash string, limit int) bool {
	return keyLimiter.Allow("fetch:"+keyHash, limit)
}

// AllowIngest consumes one per-second token for an ingest key hash on its
// own namespaced per-key bucket ("ingest:"+hash), independent of fetch.
func AllowIngest(keyHash string) bool {
	return AllowIngestKey(keyHash, GetConfig().IngestRps)
}

// AllowIngestKey consumes one per-second token for an ingest key hash at
// an explicit limit (per-key override path) on the ingest namespace.
func AllowIngestKey(keyHash string, limit int) bool {
	return keyLimiter.Allow("ingest:"+keyHash, limit)
}

// Per-key sdk_keys field names for request/sec overrides. Zero/missing
// means "fall back to the global config"; the Admin UI shows 0 = global.
const (
	// FetchRpsField overrides Config.FetchRps for one SDK key.
	FetchRpsField = "fetchRps"
	// IngestRpsField overrides Config.IngestRps for one SDK key.
	IngestRpsField = "ingestRps"
)

// effectiveRps resolves a per-key override: a value inside 1..MaxRps wins,
// anything else (missing, zero, negative, over-max — never trust the DB
// blindly) falls back to the global default.
func effectiveRps(key *core.Record, field string, global int) int {
	if key != nil {
		if v := key.GetInt(field); v >= MinRps && v <= MaxRps {
			return v
		}
	}
	return global
}

// EffectiveFetchRps returns the per-second fetch budget for key: its
// fetchRps override when valid (1..10000), else the global FetchRps.
func EffectiveFetchRps(key *core.Record) int {
	return effectiveRps(key, FetchRpsField, GetConfig().FetchRps)
}

// EffectiveIngestRps returns the per-second ingest budget for key: its
// ingestRps override when valid (1..10000), else the global IngestRps.
func EffectiveIngestRps(key *core.Record) int {
	return effectiveRps(key, IngestRpsField, GetConfig().IngestRps)
}

// AllowAdmin consumes one per-second token for an admin caller IP on a
// dedicated limiter (admin traffic never eats the global budget).
func AllowAdmin(ip string) bool {
	return adminLimiter.Allow("admin:"+ip, GetConfig().AdminRps)
}

// isTrustedProxyRemote reports whether remoteAddr (the direct TCP peer)
// is a local proxy whose forwarded headers may be trusted: loopback
// (127.0.0.0/8, ::1), private ranges (10/8, 172.16/12, 192.168/16 via
// IsPrivate), and link-local unicast (169.254/16, fe80::/10). Stdlib
// only. The host is parsed via net.SplitHostPort with fallback to the
// bare string so both "1.2.3.4:5678" and "1.2.3.4" work.
func isTrustedProxyRemote(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = strings.TrimSpace(h)
	}
	host = strings.Trim(host, "[]")
	if i := strings.LastIndex(host, "%"); i != -1 {
		host = host[:i]
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// remoteHostFromAddr extracts the host part of RemoteAddr (port
// stripped), falling back to the trimmed bare string.
func remoteHostFromAddr(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(remoteAddr)
}

// clientIPFromHeaders extracts the client IP purely (no I/O) so it is
// unit-testable. X-Forwarded-For / X-Real-IP are client-controlled and
// therefore trusted ONLY when the direct peer (RemoteAddr, set by the Go
// net stack and unspoofable) is a local proxy per isTrustedProxyRemote:
// first X-Forwarded-For entry wins, then X-Real-IP, then the remote
// host. A public (non-trusted) peer returns the remote host directly so
// spoofed headers cannot move the request into another IP's bucket.
// Empty or unparseable remote host falls back to the header-first logic
// to preserve old behavior for test-only empty cases (RemoteAddr is
// always set on real connections). Empty everywhere → "".
func clientIPFromHeaders(hdr http.Header, remoteAddr string) string {
	remoteHost := remoteHostFromAddr(remoteAddr)
	if remoteHost != "" {
		bare := strings.Trim(strings.Trim(remoteHost, "[]"), " ")
		if i := strings.LastIndex(bare, "%"); i != -1 {
			bare = bare[:i]
		}
		if net.ParseIP(bare) != nil && !isTrustedProxyRemote(remoteAddr) {
			return remoteHost
		}
		// Trusted peer, or unparseable hostname: fall through to the
		// header-first logic below (unparseable preserves old behavior).
	}
	if xff := hdr.Get("X-Forwarded-For"); xff != "" {
		if first, _, _ := strings.Cut(xff, ","); strings.TrimSpace(first) != "" {
			if cand := strings.TrimSpace(first); net.ParseIP(cand) != nil {
				return cand
			}
		}
	}
	if xr := strings.TrimSpace(hdr.Get("X-Real-IP")); xr != "" {
		if net.ParseIP(xr) != nil {
			return xr
		}
	}
	return remoteHost
}

// ClientIP reports the client IP for a request event.
func ClientIP(re *core.RequestEvent) string {
	return clientIPFromHeaders(re.Request.Header, re.Request.RemoteAddr)
}

// CheckAdmin reports whether re (an /api/v1/admin/* call) may proceed on
// the dedicated admin limiter: one per-second token for the caller IP at
// AdminRps (default 20). Pure check — the caller renders the 429 via
// BlockIP (Retry-After:1, {message, status:429}).
func CheckAdmin(re *core.RequestEvent) bool {
	return AllowAdmin(ClientIP(re))
}

// CheckIP reports whether re may proceed: non-/api/ paths always pass
// (static UI/assets are not flood-gated); /api/v1/admin/* consumes one
// admin per-IP token on the dedicated limiter (never eats the global
// budget); other /api/* consumes one global per-IP token. Pure check —
// the caller renders the 429.
func CheckIP(re *core.RequestEvent) bool {
	if !strings.HasPrefix(re.Request.URL.Path, "/api/") {
		return true
	}
	ip := ClientIP(re)
	// Fail-open on empty IP: RemoteAddr is always set by the Go net
	// stack, so "" only happens in tests; allowing it avoids collapsing
	// all headerless test clients into one shared "" bucket. Fetch/ingest
	// per-key limits still apply after auth, so this does not bypass auth.
	if ip == "" {
		return true
	}
	if strings.HasPrefix(re.Request.URL.Path, "/api/v1/admin/") {
		return AllowAdmin(ip)
	}
	return AllowIP(ip)
}

// BlockIP renders the 429 flood response (Retry-After:1, JSON shape
// {message, status:429} matching the ingest/fetch 429 shape).
func BlockIP(re *core.RequestEvent) error {
	security.SetHeaders(re)
	re.Response.Header().Set("Retry-After", "1")
	return re.JSON(http.StatusTooManyRequests, map[string]any{
		"message": "Rate limit exceeded.",
		"status":  http.StatusTooManyRequests,
	})
}

// Middleware adapts the IP check to a PocketBase router BindFunc: non-API
// paths and in-budget IPs call next; over-budget /api/* callers get the
// 429 WITHOUT reaching auth (must be bound before auth-dependent routes).
// /api/v1/admin/* is gated by the dedicated admin limiter (AdminRps per
// IP), all other /api/* by the global limiter — either 429 renders via
// BlockIP.
// Usage (wiring layer): se.Router.BindFunc(func(re *core.RequestEvent) error {
// return limits.Middleware(re.Next)(re) }) — or inline CheckIP/BlockIP.
func Middleware(next func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(re *core.RequestEvent) error {
		if !CheckIP(re) {
			return BlockIP(re)
		}
		return next(re)
	}
}

// collectionName is the PocketBase collection holding the singleton row.
const collectionName = "rate_settings"

// globalKey identifies the singleton settings row.
const globalKey = "global"

// ensureLoaded seeds the singleton rate_settings row when absent and loads
// DB values over env defaults. Missing collection (migration not yet
// applied) keeps env defaults with a warning — never fatal on serve.
func ensureLoaded(app core.App) {
	c := loadDefaultsFromEnv()
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		cfgMu.Lock()
		current = clampConfig(c)
		cfgMu.Unlock()
		log.Print("limits: rate_settings collection missing, using env defaults")
		return
	}
	rec, err := findGlobalRow(app)
	if err != nil {
		log.Printf("limits: failed to read rate_settings: %v, using env defaults", err)
		cfgMu.Lock()
		current = clampConfig(c)
		cfgMu.Unlock()
		return
	}
	if rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("key", globalKey)
		rec.Set("rps", c.GlobalRps)
		rec.Set("burst", c.Burst)
		rec.Set("fetchRps", c.FetchRps)
		rec.Set("ingestRps", c.IngestRps)
		rec.Set("adminRps", c.AdminRps)
		if err := app.Save(rec); err != nil {
			log.Printf("limits: failed to seed rate_settings: %v", err)
		}
		cfgMu.Lock()
		current = clampConfig(c)
		cfgMu.Unlock()
		return
	}
	if v := rec.GetInt("rps"); v > 0 {
		c.GlobalRps = v
	}
	if v := rec.GetInt("burst"); v > 0 {
		c.Burst = v
	}
	if v := rec.GetInt("fetchRps"); v > 0 {
		c.FetchRps = v
	}
	if v := rec.GetInt("ingestRps"); v > 0 {
		c.IngestRps = v
	}
	if v := rec.GetInt("adminRps"); v > 0 {
		c.AdminRps = v
	}
	cfgMu.Lock()
	current = clampConfig(c)
	cfgMu.Unlock()
}

// findGlobalRow returns the singleton row (key=global) or nil when absent.
func findGlobalRow(app core.App) (*core.Record, error) {
	recs, err := app.FindRecordsByFilter(
		collectionName,
		"key = {:key}",
		"",
		1, 0,
		dbx.Params{"key": globalKey},
	)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	return recs[0], nil
}
