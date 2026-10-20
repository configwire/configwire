package retention

import (
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.RawDays != DefaultRawDays || d.RollupDays != DefaultRollupDays {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if DefaultRawDays != 30 || DefaultRollupDays != 90 {
		t.Fatalf("locked defaults must be {30,90}, got %+v", d)
	}
	if err := ValidateConfig(d); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
}

func TestLoadDefaultsFromEnv(t *testing.T) {
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "45")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "120")
	c := loadDefaultsFromEnv()
	if c.RawDays != 45 || c.RollupDays != 120 {
		t.Fatalf("env overrides not applied: %+v", c)
	}
	// Invalid values fall back to defaults.
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "bogus")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "")
	if c := loadDefaultsFromEnv(); c.RawDays != DefaultRawDays || c.RollupDays != DefaultRollupDays {
		t.Fatalf("invalid env should fall back to defaults, got %+v", c)
	}
	// Unset env yields code defaults.
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "")
	if c := loadDefaultsFromEnv(); c.RawDays != 30 || c.RollupDays != 90 {
		t.Fatalf("unset env should yield {30,90}, got %+v", c)
	}
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(Defaults()); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
	bad := []Config{
		{RawDays: 0, RollupDays: 90},
		{RawDays: -1, RollupDays: 90},
		{RawDays: 1826, RollupDays: 1826},
		{RawDays: 30, RollupDays: 0},
		{RawDays: 30, RollupDays: 1826},
		{RawDays: 90, RollupDays: 30}, // rollup < raw violates invariant
		{RawDays: 2, RollupDays: 1},
	}
	for i, c := range bad {
		if err := ValidateConfig(c); err == nil {
			t.Fatalf("case %d (%+v) should be rejected", i, c)
		}
	}
	edges := []Config{
		{RawDays: 1, RollupDays: 1},
		{RawDays: 1, RollupDays: 1825},
		{RawDays: 1825, RollupDays: 1825},
		{RawDays: 30, RollupDays: 90},
	}
	for i, c := range edges {
		if err := ValidateConfig(c); err != nil {
			t.Fatalf("edge case %d (%+v) should pass: %v", i, c, err)
		}
	}
}

func TestClampConfig(t *testing.T) {
	// Out-of-range values fall back to defaults.
	got := clampConfig(Config{RawDays: 0, RollupDays: 9999})
	if got.RawDays != DefaultRawDays || got.RollupDays != DefaultRollupDays {
		t.Fatalf("out-of-range should clamp to defaults, got %+v", got)
	}
	// Invariant repair: rollup < raw resolves to rollup == raw.
	got = clampConfig(Config{RawDays: 60, RollupDays: 30})
	if got.RawDays != 60 || got.RollupDays != 60 {
		t.Fatalf("rollup<raw should clamp rollup up to raw, got %+v", got)
	}
	// Valid config survives untouched.
	got = clampConfig(Config{RawDays: 45, RollupDays: 120})
	if got.RawDays != 45 || got.RollupDays != 120 {
		t.Fatalf("valid config should survive clamp, got %+v", got)
	}
	if err := ValidateConfig(got); err != nil {
		t.Fatalf("clamped valid config should validate: %v", err)
	}
}

func TestSeedDefaults(t *testing.T) {
	// Seed path: env defaults clamped install cleanly (mirrors the
	// ensureLoaded seed-if-nil branch without needing a DB).
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "")
	seed := clampConfig(loadDefaultsFromEnv())
	if seed.RawDays != 30 || seed.RollupDays != 90 {
		t.Fatalf("seed defaults should be {30,90}, got %+v", seed)
	}
	if err := ValidateConfig(seed); err != nil {
		t.Fatalf("seed defaults should validate: %v", err)
	}
	resetForTest(seed)
	got := GetConfig()
	if got.RawDays != 30 || got.RollupDays != 90 {
		t.Fatalf("GetConfig after seed should be {30,90}, got %+v", got)
	}
}

func TestApplyAndGetConfig(t *testing.T) {
	resetForTest(Defaults())
	next := Config{RawDays: 45, RollupDays: 120}
	if err := applyConfig(next); err != nil {
		t.Fatalf("valid apply should succeed: %v", err)
	}
	if got := GetConfig(); got.RawDays != 45 || got.RollupDays != 120 {
		t.Fatalf("GetConfig should reflect apply, got %+v", got)
	}
	// Invalid apply is rejected and leaves state untouched.
	if err := applyConfig(Config{RawDays: 100, RollupDays: 50}); err == nil {
		t.Fatalf("invariant-violating apply should be rejected")
	}
	if got := GetConfig(); got.RawDays != 45 || got.RollupDays != 120 {
		t.Fatalf("rejected apply must not mutate state, got %+v", got)
	}
	resetForTest(Defaults())
}
