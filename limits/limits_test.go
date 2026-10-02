package limits

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.GlobalRps != 200 || d.Burst != 400 || d.FetchRps != 100 || d.IngestRps != 50 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if len(d.AdminAllowedIPs) != 0 {
		t.Fatalf("default admin allowlist should be empty (allow all), got %+v", d)
	}
}

func TestParseEnvInt(t *testing.T) {
	t.Setenv("CONFIGWIRE_TEST_RPS", "123")
	if got := parseEnvInt("CONFIGWIRE_TEST_RPS", 7); got != 123 {
		t.Fatalf("valid env: got %d, want 123", got)
	}
	t.Setenv("CONFIGWIRE_TEST_RPS", "")
	if got := parseEnvInt("CONFIGWIRE_TEST_RPS", 7); got != 7 {
		t.Fatalf("empty env: got %d, want 7", got)
	}
	t.Setenv("CONFIGWIRE_TEST_RPS", "not-a-number")
	if got := parseEnvInt("CONFIGWIRE_TEST_RPS", 7); got != 7 {
		t.Fatalf("invalid env: got %d, want fallback 7", got)
	}
	if got := parseEnvInt("CONFIGWIRE_TEST_RPS_UNSET_XYZ", 7); got != 7 {
		t.Fatalf("unset env: got %d, want 7", got)
	}
}

func TestLoadDefaultsFromEnv(t *testing.T) {
	t.Setenv("CONFIGWIRE_GLOBAL_RPS", "300")
	t.Setenv("CONFIGWIRE_BURST", "600")
	t.Setenv("CONFIGWIRE_FETCH_RPS", "150")
	t.Setenv("CONFIGWIRE_INGEST_RPS", "75")
	t.Setenv("CONFIGWIRE_ADMIN_ALLOWED_IPS", "10.0.0.1, 192.168.0.0/24")
	c := loadDefaultsFromEnv()
	if c.GlobalRps != 300 || c.Burst != 600 || c.FetchRps != 150 || c.IngestRps != 75 {
		t.Fatalf("env overrides not applied: %+v", c)
	}
	if len(c.AdminAllowedIPs) != 2 || c.AdminAllowedIPs[0] != "10.0.0.1" || c.AdminAllowedIPs[1] != "192.168.0.0/24" {
		t.Fatalf("admin allowlist env not applied: %+v", c)
	}
	// Invalid values fall back to defaults.
	t.Setenv("CONFIGWIRE_GLOBAL_RPS", "bogus")
	if c := loadDefaultsFromEnv(); c.GlobalRps != DefaultGlobalRps {
		t.Fatalf("invalid env should fall back to default, got %+v", c)
	}
	// Legacy CONFIGWIRE_ADMIN_RPS is deprecated and ignored: setting it
	// must not change the allowlist (or anything else).
	t.Setenv("CONFIGWIRE_ADMIN_RPS", "30")
	t.Setenv("CONFIGWIRE_ADMIN_ALLOWED_IPS", "")
	if c := loadDefaultsFromEnv(); len(c.AdminAllowedIPs) != 0 {
		t.Fatalf("legacy CONFIGWIRE_ADMIN_RPS should be ignored, got %+v", c)
	}
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(Defaults()); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
	bad := []Config{
		{GlobalRps: 0, Burst: 400, FetchRps: 100, IngestRps: 50},
		{GlobalRps: -5, Burst: 400, FetchRps: 100, IngestRps: 50},
		{GlobalRps: 10001, Burst: 400, FetchRps: 100, IngestRps: 50},
		{GlobalRps: 200, Burst: 0, FetchRps: 100, IngestRps: 50},
		{GlobalRps: 200, Burst: 400, FetchRps: -1, IngestRps: 50},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50000},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{"not-an-ip"}},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{"1.2.3.4/33"}},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{"2001:db8::/129"}},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{""}},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: manyIPs(33)},
	}
	for i, c := range bad {
		if err := ValidateConfig(c); err == nil {
			t.Fatalf("case %d (%+v) should be rejected", i, c)
		}
	}
	edges := []Config{
		{GlobalRps: 1, Burst: 1, FetchRps: 1, IngestRps: 1},
		{GlobalRps: 10000, Burst: 10000, FetchRps: 10000, IngestRps: 10000},
		// Empty allowlist = allow all (safe upgrade).
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{}},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{"1.2.3.4", "10.0.0.0/8", "::1", "2001:db8::/32"}},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: manyIPs(32)},
	}
	for i, c := range edges {
		if err := ValidateConfig(c); err != nil {
			t.Fatalf("edge case %d (%+v) should pass: %v", i, c, err)
		}
	}
}

// manyIPs builds n distinct valid /32 entries for allowlist size tests.
func manyIPs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "10.0."+itoa(i/256)+"."+itoa(i%256))
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestClientIPFromHeaders(t *testing.T) {
	cases := []struct {
		name   string
		header map[string]string
		remote string
		want   string
	}{
		// Public peer: spoofed XFF is ignored, remote host wins (spoof fix).
		{"public remote ignores xff", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "9.9.9.9:1234", "9.9.9.9"},
		{"xff single empty remote falls back", map[string]string{"X-Forwarded-For": "10.0.0.1"}, "", "10.0.0.1"},
		// Public peer: spoofed X-Real-IP is ignored, remote host wins.
		{"public remote ignores real ip", map[string]string{"X-Real-Ip": "2.3.4.5"}, "9.9.9.9:1234", "9.9.9.9"},
		{"remote addr host", nil, "192.168.1.7:8090", "192.168.1.7"},
		{"remote addr bare", nil, "192.168.1.7", "192.168.1.7"},
		{"empty", nil, "", ""},
		// Trusted local proxy peer: forwarded headers are honored.
		{"loopback remote trusts xff", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "127.0.0.1:1234", "1.2.3.4"},
		{"private 10.x remote trusts xff", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "10.0.0.5:1234", "1.2.3.4"},
		{"private 192.168.x remote trusts xff", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "192.168.1.7:8090", "1.2.3.4"},
		{"loopback remote trusts real ip", map[string]string{"X-Real-Ip": "2.3.4.5"}, "127.0.0.1:1234", "2.3.4.5"},
		// Trusted local proxy peer with unparseable forwarded values:
		// headers must NOT become bucket keys, fall back instead.
		{"loopback ignores non-ip xff", map[string]string{"X-Forwarded-For": "not-an-ip"}, "127.0.0.1:1234", "127.0.0.1"},
		{"loopback ignores xff with port", map[string]string{"X-Forwarded-For": "1.2.3.4:9999"}, "127.0.0.1:1234", "127.0.0.1"},
		{"loopback ignores non-ip real ip", map[string]string{"X-Real-Ip": "not-an-ip"}, "127.0.0.1:1234", "127.0.0.1"},
		{"loopback invalid xff falls through to valid real ip", map[string]string{"X-Forwarded-For": "garbage", "X-Real-Ip": "2.3.4.5"}, "127.0.0.1:1234", "2.3.4.5"},
	}
	for _, tc := range cases {
		hdr := http.Header{}
		for k, v := range tc.header {
			hdr.Set(k, v)
		}
		if got := clientIPFromHeaders(hdr, tc.remote); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCheckIPEmptyIPSharesBucket pins that an empty client IP is
// rate-limited in one shared "unknown" bucket instead of fail-opening:
// the first two /api/* requests pass (budget 2) and the third in the
// same window is blocked, while non-API paths still always pass.
func TestCheckIPEmptyIPSharesBucket(t *testing.T) {
	resetForTest(Config{GlobalRps: 2, Burst: 2, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil})
	newAPIEvent := func() *core.RequestEvent {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/env/dev/config", nil)
		req.RemoteAddr = ""
		re := &core.RequestEvent{}
		re.Request = req
		return re
	}
	if !CheckIP(newAPIEvent()) || !CheckIP(newAPIEvent()) {
		t.Fatal("first two empty-IP requests should pass")
	}
	if CheckIP(newAPIEvent()) {
		t.Fatal("third empty-IP request in the same window should be blocked")
	}
	staticReq := httptest.NewRequest(http.MethodGet, "/", nil)
	staticReq.RemoteAddr = ""
	staticRe := &core.RequestEvent{}
	staticRe.Request = staticReq
	if !CheckIP(staticRe) {
		t.Fatal("non-API path should always pass")
	}
}

// TestCheckIPAdminAllowlist pins the admin-path allowlist gate: an
// allowed client IP passes /api/v1/admin/* while a denied one is
// blocked, independent of the global rate budget.
func TestCheckIPAdminAllowlist(t *testing.T) {
	newAdminEvent := func(remote string) *core.RequestEvent {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/limits", nil)
		req.RemoteAddr = remote
		re := &core.RequestEvent{}
		re.Request = req
		return re
	}
	// Empty allowlist = allow all (safe upgrade): any IP passes.
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil})
	if !CheckIP(newAdminEvent("203.0.113.9:1234")) {
		t.Fatal("empty allowlist should allow any admin IP")
	}
	// Non-empty allowlist: exact match passes, others blocked.
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: []string{"1.2.3.4", "10.0.0.0/8"}})
	if !CheckIP(newAdminEvent("1.2.3.4:1234")) {
		t.Fatal("exact allowlisted admin IP should pass")
	}
	if !CheckIP(newAdminEvent("10.7.8.9:1234")) {
		t.Fatal("CIDR-allowlisted admin IP should pass")
	}
	if CheckIP(newAdminEvent("203.0.113.9:1234")) {
		t.Fatal("non-allowlisted admin IP should be blocked")
	}
}

func TestAllowBurstBehavior(t *testing.T) {
	resetForTest(Config{GlobalRps: 3, Burst: 3, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil})
	ip := "203.0.113.9"
	// First 3 pass (valid single request passes), 4th is the flood → blocked.
	for i := 0; i < 3; i++ {
		if !AllowIP(ip) {
			t.Fatalf("request %d should pass", i+1)
		}
	}
	if AllowIP(ip) {
		t.Fatal("4th request in the same 1s window should be blocked (429 path)")
	}
}

func TestAllowBurstCeiling(t *testing.T) {
	// Burst < GlobalRps → Burst is the ceiling.
	resetForTest(Config{GlobalRps: 100, Burst: 2, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil})
	ip := "198.51.100.4"
	if !AllowIP(ip) || !AllowIP(ip) {
		t.Fatal("first two should pass")
	}
	if AllowIP(ip) {
		t.Fatal("third should hit the burst ceiling")
	}
	// Window expiry resets the budget.
	l := newLimiterWithWindow(20 * time.Millisecond)
	if !l.Allow("k", 1) || l.Allow("k", 1) {
		t.Fatal("fixed window should block second hit")
	}
	time.Sleep(30 * time.Millisecond)
	if !l.Allow("k", 1) {
		t.Fatal("new window should pass again")
	}
}

func TestAllowFetchIngest(t *testing.T) {
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 2, IngestRps: 1, AdminAllowedIPs: nil})
	if !AllowFetch("hash-a") || !AllowFetch("hash-a") {
		t.Fatal("fetch within budget should pass")
	}
	if AllowFetch("hash-a") {
		t.Fatal("fetch over budget should block")
	}
	if !AllowIngest("hash-b") {
		t.Fatal("first ingest should pass")
	}
	if AllowIngest("hash-b") {
		t.Fatal("second ingest in window should block")
	}
	// Per-key isolation: a different hash has its own budget.
	if !AllowFetch("hash-other") {
		t.Fatal("different key hash should have own budget")
	}
}

// TestAdminAllowlist pins the allowlist semantics: empty allows any IP
// (including ""), exact IPs and CIDRs (v4 + v6) match, non-matches deny,
// and an unparseable client IP denies when the list is non-empty. There
// is no admin req/s rate limiting: repeated calls from an allowed IP
// always pass.
func TestAdminAllowlist(t *testing.T) {
	// Empty allowlist = allow all.
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil})
	for _, ip := range []string{"1.2.3.4", "2001:db8::1", ""} {
		if !IsAdminIPAllowed(ip) {
			t.Fatalf("empty allowlist should allow %q", ip)
		}
		if !CheckAdmin(adminEvent(ip)) {
			t.Fatalf("empty allowlist CheckAdmin should allow %q", ip)
		}
	}

	resetForTest(Config{
		GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50,
		AdminAllowedIPs: []string{"1.2.3.4", "10.0.0.0/8", "::1", "2001:db8::/32"},
	})
	allowed := []string{"1.2.3.4", "10.7.8.9", "::1", "2001:db8::99"}
	for _, ip := range allowed {
		if !IsAdminIPAllowed(ip) {
			t.Errorf("allowlisted IP %q should be allowed", ip)
		}
		if !CheckAdmin(adminEvent(ip)) {
			t.Errorf("CheckAdmin should allow %q", ip)
		}
	}
	denied := []string{"9.9.9.9", "11.0.0.1", "::2", "2001:db9::1", "not-an-ip", ""}
	for _, ip := range denied {
		if IsAdminIPAllowed(ip) {
			t.Errorf("non-allowlisted IP %q should be denied", ip)
		}
		if CheckAdmin(adminEvent(ip)) {
			t.Errorf("CheckAdmin should deny %q", ip)
		}
	}
	// No admin req/s limiting: an allowed IP never exhausts a budget.
	for i := 0; i < 50; i++ {
		if !CheckAdmin(adminEvent("1.2.3.4")) {
			t.Fatalf("allowed admin IP should never be rate-limited (hit %d)", i+1)
		}
	}
}

// adminEvent builds an /api/v1/admin/* request event whose resolved
// client IP is ip (loopback remote trusts the forwarded header; bare
// IPs go through RemoteAddr).
func adminEvent(ip string) *core.RequestEvent {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/limits", nil)
	if ip == "" {
		req.RemoteAddr = ""
	} else if strings.Contains(ip, ":") && strings.Count(ip, ":") > 1 {
		// IPv6 literal: carry via a trusted header so resolution is exact.
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("CF-Connecting-IP", ip)
	} else if strings.Contains(ip, ".") && strings.Count(ip, ".") == 3 && !strings.Contains(ip, ":") {
		req.RemoteAddr = ip + ":1234"
	} else {
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("CF-Connecting-IP", ip)
	}
	re := &core.RequestEvent{}
	re.Request = req
	return re
}

func TestFetchIngestBudgetIndependence(t *testing.T) {
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 2, IngestRps: 2, AdminAllowedIPs: nil})
	hash := "shared-hash"
	// Exhaust the fetch budget for the shared hash.
	if !AllowFetch(hash) || !AllowFetch(hash) {
		t.Fatal("fetch within budget should pass")
	}
	if AllowFetch(hash) {
		t.Fatal("fetch over budget should block")
	}
	// Ingest on the SAME hash must be unaffected.
	if !AllowIngest(hash) || !AllowIngest(hash) {
		t.Fatal("ingest on same hash should have independent budget")
	}
	if AllowIngest(hash) {
		t.Fatal("ingest over its own budget should block")
	}
	// Vice versa with a fresh hash: exhaust ingest first, fetch unaffected.
	other := "shared-hash-2"
	if !AllowIngest(other) || !AllowIngest(other) {
		t.Fatal("ingest within budget should pass")
	}
	if AllowIngest(other) {
		t.Fatal("ingest over budget should block")
	}
	if !AllowFetch(other) || !AllowFetch(other) {
		t.Fatal("fetch on same hash should have independent budget")
	}
	if AllowFetch(other) {
		t.Fatal("fetch over its own budget should block")
	}
}

func keyWithRps(t *testing.T, fetch, ingest int, set bool) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("sdk_keys")
	col.Fields.Add(
		&core.NumberField{Name: "fetchRps", OnlyInt: true},
		&core.NumberField{Name: "ingestRps", OnlyInt: true},
	)
	rec := core.NewRecord(col)
	if set {
		rec.Set("fetchRps", fetch)
		rec.Set("ingestRps", ingest)
	}
	return rec
}

func TestEffectiveFetchIngestRps(t *testing.T) {
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil})

	// Nil key and missing fields fall back to global.
	if got := EffectiveFetchRps(nil); got != 100 {
		t.Fatalf("EffectiveFetchRps(nil) = %d, want global 100", got)
	}
	if got := EffectiveIngestRps(nil); got != 50 {
		t.Fatalf("EffectiveIngestRps(nil) = %d, want global 50", got)
	}
	if got := EffectiveFetchRps(keyWithRps(t, 0, 0, false)); got != 100 {
		t.Fatalf("EffectiveFetchRps(missing) = %d, want global 100", got)
	}
	if got := EffectiveIngestRps(keyWithRps(t, 0, 0, false)); got != 50 {
		t.Fatalf("EffectiveIngestRps(missing) = %d, want global 50", got)
	}

	// Valid overrides win.
	key := keyWithRps(t, 7, 9, true)
	if got := EffectiveFetchRps(key); got != 7 {
		t.Fatalf("EffectiveFetchRps(override 7) = %d, want 7", got)
	}
	if got := EffectiveIngestRps(key); got != 9 {
		t.Fatalf("EffectiveIngestRps(override 9) = %d, want 9", got)
	}

	// Zero/negative/missing per-key values fall back to global.
	for _, v := range []int{0, -1, -100} {
		if got := EffectiveFetchRps(keyWithRps(t, v, v, true)); got != 100 {
			t.Fatalf("EffectiveFetchRps(%d) = %d, want global 100", v, got)
		}
		if got := EffectiveIngestRps(keyWithRps(t, v, v, true)); got != 50 {
			t.Fatalf("EffectiveIngestRps(%d) = %d, want global 50", v, got)
		}
	}

	// Out-of-range high values clamp to global (never trust the DB).
	if got := EffectiveFetchRps(keyWithRps(t, 10001, 50000, true)); got != 100 {
		t.Fatalf("EffectiveFetchRps(over-max) = %d, want global 100", got)
	}
	if got := EffectiveIngestRps(keyWithRps(t, 10001, 50000, true)); got != 50 {
		t.Fatalf("EffectiveIngestRps(over-max) = %d, want global 50", got)
	}

	// Boundary values 1 and 10000 are valid overrides.
	edge := keyWithRps(t, 1, 10000, true)
	if got := EffectiveFetchRps(edge); got != 1 {
		t.Fatalf("EffectiveFetchRps(1) = %d, want 1", got)
	}
	if got := EffectiveIngestRps(edge); got != 10000 {
		t.Fatalf("EffectiveIngestRps(10000) = %d, want 10000", got)
	}
}
