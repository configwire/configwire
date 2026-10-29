package logstore

import (
	"strings"
	"testing"
)

func TestCeilingDefaultsToMaxDays(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_LOG_DAYS", "")
	if got := CeilingDays(); got != MaxDays {
		t.Fatalf("CeilingDays unset = %d, want %d", got, MaxDays)
	}
	t.Setenv("CONFIGWIRE_MAX_LOG_DAYS", "bogus")
	if got := CeilingDays(); got != MaxDays {
		t.Fatalf("CeilingDays malformed = %d, want %d", got, MaxDays)
	}
}

func TestValidateSettingsCeilingRejects(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_LOG_DAYS", "7")
	// At-ceiling values validate (this is what PUT accepts).
	if err := ValidateSettings(Settings{Enabled: true, MaxDays: 7, SuccessLogs: true}); err != nil {
		t.Fatalf("at-ceiling settings should validate: %v", err)
	}
	// Above-ceiling values fail 400-style with the ceiling named (this is
	// what PUT /api/v1/admin/log-settings renders).
	err := ValidateSettings(Settings{Enabled: true, MaxDays: 8, SuccessLogs: true})
	if err == nil {
		t.Fatal("maxDays above ceiling should be rejected")
	}
	if !strings.Contains(err.Error(), "(plan ceiling)") {
		t.Fatalf("error should name the ceiling, got %q", err.Error())
	}
}

func TestValidateSettingsNoCeilingPreservesShape(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_LOG_DAYS", "")
	err := ValidateSettings(Settings{Enabled: true, MaxDays: 1826, SuccessLogs: true})
	if err == nil || err.Error() != "invalid maxDays: must be 1..1825." {
		t.Fatalf("unset ceiling must keep the absolute message, got %v", err)
	}
	if err := ValidateSettings(Settings{Enabled: true, MaxDays: 1825, SuccessLogs: true}); err != nil {
		t.Fatalf("1825 without ceiling should validate: %v", err)
	}
}

func TestClampSettingsPullsAboveCeilingDown(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_LOG_DAYS", "7")
	// Pre-existing above-ceiling DB value clamps down to the ceiling.
	got := clampSettings(Settings{Enabled: true, MaxDays: 30, SuccessLogs: true})
	if got.MaxDays != 7 {
		t.Fatalf("clampSettings should pull 30 down to ceiling 7, got %+v", got)
	}
	if err := ValidateSettings(got); err != nil {
		t.Fatalf("clamped settings should validate: %v", err)
	}
	// Out-of-range absolute values still fall back to defaults.
	got = clampSettings(Settings{Enabled: true, MaxDays: 9999, SuccessLogs: true})
	if got.MaxDays != DefaultMaxDays {
		t.Fatalf("out-of-range should clamp to default %d, got %+v", DefaultMaxDays, got)
	}
}
