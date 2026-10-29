package limits

import (
	"strings"
	"testing"
)

func clearCeilingEnvs(t *testing.T) {
	t.Helper()
	t.Setenv("CONFIGWIRE_MAX_GLOBAL_RPS", "")
	t.Setenv("CONFIGWIRE_MAX_BURST", "")
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "")
}

func TestCeilingDefaultsToMaxRps(t *testing.T) {
	clearCeilingEnvs(t)
	if got := CeilingGlobalRps(); got != MaxRps {
		t.Fatalf("CeilingGlobalRps unset = %d, want %d", got, MaxRps)
	}
	if got := CeilingBurst(); got != MaxRps {
		t.Fatalf("CeilingBurst unset = %d, want %d", got, MaxRps)
	}
	if got := CeilingFetchRps(); got != MaxRps {
		t.Fatalf("CeilingFetchRps unset = %d, want %d", got, MaxRps)
	}
	if got := CeilingIngestRps(); got != MaxRps {
		t.Fatalf("CeilingIngestRps unset = %d, want %d", got, MaxRps)
	}
	// Malformed values are not a ceiling either.
	t.Setenv("CONFIGWIRE_MAX_GLOBAL_RPS", "bogus")
	t.Setenv("CONFIGWIRE_MAX_BURST", "12.5")
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "  ")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "abc!")
	if got := CeilingGlobalRps(); got != MaxRps {
		t.Fatalf("CeilingGlobalRps malformed = %d, want %d", got, MaxRps)
	}
	if got := CeilingBurst(); got != MaxRps {
		t.Fatalf("CeilingBurst malformed = %d, want %d", got, MaxRps)
	}
	if got := CeilingFetchRps(); got != MaxRps {
		t.Fatalf("CeilingFetchRps malformed = %d, want %d", got, MaxRps)
	}
	if got := CeilingIngestRps(); got != MaxRps {
		t.Fatalf("CeilingIngestRps malformed = %d, want %d", got, MaxRps)
	}
}

func TestValidateConfigCeilingRejects(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_GLOBAL_RPS", "50")
	t.Setenv("CONFIGWIRE_MAX_BURST", "50")
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "10")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "5")
	// At-ceiling values validate (this is what PUT accepts).
	atCeiling := Config{GlobalRps: 50, Burst: 50, FetchRps: 10, IngestRps: 5}
	if err := ValidateConfig(atCeiling); err != nil {
		t.Fatalf("at-ceiling config should validate: %v", err)
	}
	// Above-ceiling values fail 400-style with the ceiling named (this is
	// what PUT /api/v1/admin/limits renders via putLimits).
	cases := []struct {
		name string
		cfg  Config
	}{
		{"globalRps", Config{GlobalRps: 51, Burst: 50, FetchRps: 10, IngestRps: 5}},
		{"burst", Config{GlobalRps: 50, Burst: 51, FetchRps: 10, IngestRps: 5}},
		{"fetchRps", Config{GlobalRps: 50, Burst: 50, FetchRps: 11, IngestRps: 5}},
		{"ingestRps", Config{GlobalRps: 50, Burst: 50, FetchRps: 10, IngestRps: 6}},
	}
	for _, tc := range cases {
		err := ValidateConfig(tc.cfg)
		if err == nil {
			t.Fatalf("%s above ceiling should be rejected", tc.name)
		}
		if !strings.Contains(err.Error(), "(plan ceiling)") {
			t.Fatalf("%s error should name the ceiling, got %q", tc.name, err.Error())
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Fatalf("%s error should name the field, got %q", tc.name, err.Error())
		}
	}
}

func TestValidateConfigNoCeilingPreservesShape(t *testing.T) {
	clearCeilingEnvs(t)
	err := ValidateConfig(Config{GlobalRps: 10001, Burst: 400, FetchRps: 100, IngestRps: 50})
	if err == nil || err.Error() != "invalid globalRps: must be 1..10000." {
		t.Fatalf("unset ceiling must keep the absolute message, got %v", err)
	}
	edges := Config{GlobalRps: 10000, Burst: 10000, FetchRps: 10000, IngestRps: 10000}
	if err := ValidateConfig(edges); err != nil {
		t.Fatalf("10000 without ceiling should validate: %v", err)
	}
}

func TestEffectiveRpsCapsOverrideAtCeiling(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "50")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "5")
	resetForTest(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50})
	// Override above the ceiling yields the ceiling, not the global.
	if got := EffectiveFetchRps(keyWithRps(t, 5000, 5000, true)); got != 50 {
		t.Fatalf("EffectiveFetchRps(5000, ceiling 50) = %d, want 50", got)
	}
	if got := EffectiveIngestRps(keyWithRps(t, 5000, 5000, true)); got != 5 {
		t.Fatalf("EffectiveIngestRps(5000, ceiling 5) = %d, want 5", got)
	}
	// Below-ceiling overrides still win; 0/missing/over-max still fall back.
	if got := EffectiveFetchRps(keyWithRps(t, 7, 7, true)); got != 7 {
		t.Fatalf("EffectiveFetchRps(7) = %d, want 7", got)
	}
	if got := EffectiveIngestRps(keyWithRps(t, 5, 5, true)); got != 5 {
		t.Fatalf("EffectiveIngestRps(5) = %d, want 5", got)
	}
	if got := EffectiveFetchRps(keyWithRps(t, 0, 0, true)); got != 100 {
		t.Fatalf("EffectiveFetchRps(0) = %d, want global 100", got)
	}
	if got := EffectiveFetchRps(keyWithRps(t, 10001, 10001, true)); got != 100 {
		t.Fatalf("EffectiveFetchRps(10001) = %d, want global 100", got)
	}
}

func TestClampConfigPullsAboveCeilingDown(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_GLOBAL_RPS", "")
	t.Setenv("CONFIGWIRE_MAX_BURST", "")
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "100")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "")
	// Pre-existing above-ceiling DB value clamps down to the ceiling.
	got := clampConfig(Config{GlobalRps: 200, Burst: 400, FetchRps: 5000, IngestRps: 50})
	if got.FetchRps != 100 {
		t.Fatalf("clampConfig should pull 5000 down to ceiling 100, got %+v", got)
	}
	if got.GlobalRps != 200 || got.Burst != 400 || got.IngestRps != 50 {
		t.Fatalf("clampConfig must leave in-ceiling fields alone, got %+v", got)
	}
	// Out-of-range absolute values still fall back to defaults.
	got = clampConfig(Config{GlobalRps: 200, Burst: 400, FetchRps: 20000, IngestRps: 50})
	if got.FetchRps != DefaultFetchRps {
		t.Fatalf("out-of-range should fall back to default %d, got %+v", DefaultFetchRps, got)
	}
}

func TestCeilingBelowMinStillEnforces(t *testing.T) {
	clearCeilingEnvs(t)
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "0")
	err := ValidateConfig(Config{GlobalRps: 200, Burst: 400, FetchRps: 1, IngestRps: 50})
	if err == nil {
		t.Fatal("ceiling 0 must reject even fetchRps=1")
	}
	if !strings.Contains(err.Error(), "(plan ceiling)") {
		t.Fatalf("misconfigured ceiling error should name the ceiling, got %q", err.Error())
	}
}
