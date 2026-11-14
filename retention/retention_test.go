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
	// Single-knob env: retentionDays drives both windows.
	t.Setenv("CONFIGWIRE_RETENTION_DAYS", "120")
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "")
	c := loadDefaultsFromEnv()
	if c.RawDays != 30 || c.RollupDays != 120 {
		t.Fatalf("single-knob env should derive {30,120}, got %+v", c)
	}
	// Short retention: raw capped to the total retention, no rollups.
	t.Setenv("CONFIGWIRE_RETENTION_DAYS", "7")
	c = loadDefaultsFromEnv()
	if c.RawDays != 7 || c.RollupDays != 7 {
		t.Fatalf("short retention should derive {7,7}, got %+v", c)
	}
	// Legacy advanced envs: raw capped at MaxRawDays.
	t.Setenv("CONFIGWIRE_RETENTION_DAYS", "")
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "45")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "120")
	c = loadDefaultsFromEnv()
	if c.RawDays != 30 || c.RollupDays != 120 {
		t.Fatalf("legacy env overrides should clamp raw to 30, got %+v", c)
	}
	// Invalid values fall back to defaults.
	t.Setenv("CONFIGWIRE_RETENTION_DAYS", "bogus")
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "bogus")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "")
	if c := loadDefaultsFromEnv(); c.RawDays != DefaultRawDays || c.RollupDays != DefaultRollupDays {
		t.Fatalf("invalid env should fall back to defaults, got %+v", c)
	}
	// A set-but-unparseable single knob falls through to the legacy pair
	// instead of silently discarding it (migration typo keeps legacy tuning).
	t.Setenv("CONFIGWIRE_RETENTION_DAYS", "bogus")
	t.Setenv("CONFIGWIRE_RAW_RETENTION_DAYS", "45")
	t.Setenv("CONFIGWIRE_ROLLUP_RETENTION_DAYS", "120")
	if c := loadDefaultsFromEnv(); c.RawDays != 30 || c.RollupDays != 120 {
		t.Fatalf("bogus single knob should fall through to legacy envs, got %+v", c)
	}
	// Unset env yields code defaults.
	t.Setenv("CONFIGWIRE_RETENTION_DAYS", "")
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
		{RawDays: 31, RollupDays: 90}, // raw above MaxRawDays
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
		{RawDays: 30, RollupDays: 30},
		{RawDays: 30, RollupDays: 1825},
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
	// Invariant repair: raw capped at min(rollup, MaxRawDays).
	got = clampConfig(Config{RawDays: 60, RollupDays: 30})
	if got.RawDays != 30 || got.RollupDays != 30 {
		t.Fatalf("rollup<raw should clamp raw to min(rollup,30), got %+v", got)
	}
	// Raw above MaxRawDays is clamped down.
	got = clampConfig(Config{RawDays: 45, RollupDays: 120})
	if got.RawDays != 30 || got.RollupDays != 120 {
		t.Fatalf("raw>30 should clamp raw to 30, got %+v", got)
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
	ResetForTest(seed)
	got := GetConfig()
	if got.RawDays != 30 || got.RollupDays != 90 {
		t.Fatalf("GetConfig after seed should be {30,90}, got %+v", got)
	}
}

func TestApplyAndGetConfig(t *testing.T) {
	ResetForTest(Defaults())
	next := Config{RawDays: 30, RollupDays: 120}
	if err := applyConfig(next); err != nil {
		t.Fatalf("valid apply should succeed: %v", err)
	}
	if got := GetConfig(); got.RawDays != 30 || got.RollupDays != 120 {
		t.Fatalf("GetConfig should reflect apply, got %+v", got)
	}
	// Invalid apply is rejected and leaves state untouched.
	if err := applyConfig(Config{RawDays: 100, RollupDays: 50}); err == nil {
		t.Fatalf("invariant-violating apply should be rejected")
	}
	if got := GetConfig(); got.RawDays != 30 || got.RollupDays != 120 {
		t.Fatalf("rejected apply must not mutate state, got %+v", got)
	}
	ResetForTest(Defaults())
}

func TestEffectiveRawDaysHonorsOverride(t *testing.T) {
	// Advanced override: stored rawDays < 30 shortens the raw window.
	if got := (Config{RawDays: 14, RollupDays: 120}).EffectiveRawDays(); got != 14 {
		t.Fatalf("EffectiveRawDays({14,120}) = %d, want 14", got)
	}
	if got := (Config{RawDays: 30, RollupDays: 90}).EffectiveRawDays(); got != 30 {
		t.Fatalf("EffectiveRawDays({30,90}) = %d, want 30", got)
	}
	if got := (Config{RawDays: 7, RollupDays: 7}).EffectiveRawDays(); got != 7 {
		t.Fatalf("EffectiveRawDays({7,7}) = %d, want 7", got)
	}
	// Invalid stored values fall back to the derived window, never 0.
	if got := (Config{RawDays: 0, RollupDays: 0}).EffectiveRawDays(); got != MaxRawDays {
		t.Fatalf("EffectiveRawDays({0,0}) = %d, want %d", got, MaxRawDays)
	}
	if got := (Config{RawDays: 30, RollupDays: 7}).EffectiveRawDays(); got != 7 {
		t.Fatalf("EffectiveRawDays({30,7}) = %d, want 7", got)
	}
}
