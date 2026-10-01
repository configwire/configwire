package limits

import (
	"net/http"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.GlobalRps != 200 || d.Burst != 400 || d.FetchRps != 100 || d.IngestRps != 50 || d.AdminRps != 20 {
		t.Fatalf("unexpected defaults: %+v", d)
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
	t.Setenv("CONFIGWIRE_ADMIN_RPS", "30")
	c := loadDefaultsFromEnv()
	if c.GlobalRps != 300 || c.Burst != 600 || c.FetchRps != 150 || c.IngestRps != 75 || c.AdminRps != 30 {
		t.Fatalf("env overrides not applied: %+v", c)
	}
	// Invalid values fall back to defaults.
	t.Setenv("CONFIGWIRE_GLOBAL_RPS", "bogus")
	if c := loadDefaultsFromEnv(); c.GlobalRps != DefaultGlobalRps {
		t.Fatalf("invalid env should fall back to default, got %+v", c)
	}
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(Defaults()); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
	bad := []Config{
		{GlobalRps: 0, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 20},
		{GlobalRps: -5, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 20},
		{GlobalRps: 10001, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 20},
		{GlobalRps: 200, Burst: 0, FetchRps: 100, IngestRps: 50, AdminRps: 20},
		{GlobalRps: 200, Burst: 400, FetchRps: -1, IngestRps: 50, AdminRps: 20},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50000, AdminRps: 20},
		{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 0},
	}
	for i, c := range bad {
		if err := ValidateConfig(c); err == nil {
			t.Fatalf("case %d (%+v) should be rejected", i, c)
		}
	}
	edges := []Config{
		{GlobalRps: 1, Burst: 1, FetchRps: 1, IngestRps: 1, AdminRps: 1},
		{GlobalRps: 10000, Burst: 10000, FetchRps: 10000, IngestRps: 10000, AdminRps: 10000},
	}
	for i, c := range edges {
		if err := ValidateConfig(c); err != nil {
			t.Fatalf("edge case %d (%+v) should pass: %v", i, c, err)
		}
	}
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

func TestAllowBurstBehavior(t *testing.T) {
	resetForTest(Config{GlobalRps: 3, Burst: 3, FetchRps: 100, IngestRps: 50, AdminRps: 20})
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
	resetForTest(Config{GlobalRps: 100, Burst: 2, FetchRps: 100, IngestRps: 50, AdminRps: 20})
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

func TestAllowFetchIngestAdmin(t *testing.T) {
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 2, IngestRps: 1, AdminRps: 1})
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
	if !AllowAdmin("10.1.1.1") {
		t.Fatal("first admin should pass")
	}
	if AllowAdmin("10.1.1.1") {
		t.Fatal("second admin in window should block")
	}
	// Per-key isolation: a different hash has its own budget.
	if !AllowFetch("hash-other") {
		t.Fatal("different key hash should have own budget")
	}
}

func TestFetchIngestBudgetIndependence(t *testing.T) {
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 2, IngestRps: 2, AdminRps: 20})
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
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 20})

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
