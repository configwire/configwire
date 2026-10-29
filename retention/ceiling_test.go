package retention

import (
	"strings"
	"testing"
)

func TestCeilingDefaultsToMaxDays(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_RETENTION_DAYS", "")
	if got := CeilingDays(); got != MaxDays {
		t.Fatalf("CeilingDays unset = %d, want %d", got, MaxDays)
	}
	t.Setenv("CONFIGWIRE_MAX_RETENTION_DAYS", "bogus")
	if got := CeilingDays(); got != MaxDays {
		t.Fatalf("CeilingDays malformed = %d, want %d", got, MaxDays)
	}
}

func TestValidateConfigCeilingRejects(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_RETENTION_DAYS", "7")
	// At-ceiling values validate (this is what PUT accepts).
	if err := ValidateConfig(Config{RawDays: 7, RollupDays: 7}); err != nil {
		t.Fatalf("at-ceiling config should validate: %v", err)
	}
	// Above-ceiling values fail 400-style with the ceiling named (this is
	// what PUT /api/v1/admin/retention renders).
	for _, c := range []Config{
		{RawDays: 8, RollupDays: 8},
		{RawDays: 7, RollupDays: 8},
	} {
		err := ValidateConfig(c)
		if err == nil {
			t.Fatalf("%+v above ceiling should be rejected", c)
		}
		if !strings.Contains(err.Error(), "(plan ceiling)") {
			t.Fatalf("%+v error should name the ceiling, got %q", c, err.Error())
		}
	}
}

func TestValidateConfigNoCeilingPreservesShape(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_RETENTION_DAYS", "")
	err := ValidateConfig(Config{RawDays: 1826, RollupDays: 1826})
	if err == nil || err.Error() != "invalid rawDays: must be 1..1825." {
		t.Fatalf("unset ceiling must keep the absolute message, got %v", err)
	}
	if err := ValidateConfig(Config{RawDays: 30, RollupDays: 1825}); err != nil {
		t.Fatalf("1825 without ceiling should validate: %v", err)
	}
}

func TestClampConfigPullsAboveCeilingDown(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_RETENTION_DAYS", "7")
	// Pre-existing above-ceiling DB values clamp down to the ceiling.
	got := clampConfig(Config{RawDays: 30, RollupDays: 90})
	if got.RawDays != 7 || got.RollupDays != 7 {
		t.Fatalf("clampConfig should pull {30,90} down to ceiling 7, got %+v", got)
	}
	if err := ValidateConfig(got); err != nil {
		t.Fatalf("clamped config should validate: %v", err)
	}
	// Out-of-range absolute values still fall back to defaults.
	got = clampConfig(Config{RawDays: 0, RollupDays: 9999})
	if got.RawDays != DefaultRawDays || got.RollupDays != DefaultRollupDays {
		t.Fatalf("out-of-range should clamp to defaults, got %+v", got)
	}
}
