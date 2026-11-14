package retention

import (
	"testing"
)

func intPtr(n int) *int { return &n }

func TestToBodyShape(t *testing.T) {
	body := toBody(Config{RawDays: 30, RollupDays: 120})
	if body["retentionDays"] != 120 {
		t.Fatalf("retentionDays should equal rollupDays: %+v", body)
	}
	if body["rawDays"] != 30 || body["rollupDays"] != 120 {
		t.Fatalf("unexpected body shape: %+v", body)
	}
}

func TestResolveRetentionPUTSingleKnob(t *testing.T) {
	stored := Config{RawDays: 30, RollupDays: 90}
	// retentionDays alone derives rawDays as min(retentionDays, MaxRawDays).
	next, err := resolveRetentionPUT(stored, retentionBody{RetentionDays: intPtr(120)})
	if err != nil {
		t.Fatalf("single-knob PUT should resolve: %v", err)
	}
	if next.RawDays != 30 || next.RollupDays != 120 {
		t.Fatalf("120d retention should derive {30,120}, got %+v", next)
	}

	next, err = resolveRetentionPUT(stored, retentionBody{RetentionDays: intPtr(7)})
	if err != nil {
		t.Fatalf("short single-knob PUT should resolve: %v", err)
	}
	if next.RawDays != 7 || next.RollupDays != 7 {
		t.Fatalf("7d retention should derive {7,7}, got %+v", next)
	}
}

func TestResolveRetentionPUTAdvancedPair(t *testing.T) {
	stored := Config{RawDays: 30, RollupDays: 90}
	// Legacy advanced pair: rollupDays is the total retention; rawDays is
	// capped by validation but accepted verbatim here.
	next, err := resolveRetentionPUT(stored, retentionBody{RollupDays: intPtr(120)})
	if err != nil {
		t.Fatalf("advanced rollupDays PUT should resolve: %v", err)
	}
	if next.RawDays != 30 || next.RollupDays != 120 {
		t.Fatalf("rollupDays=120 should keep raw=30, got %+v", next)
	}

	// Explicit rawDays is preserved for advanced overrides.
	next, err = resolveRetentionPUT(stored, retentionBody{RawDays: intPtr(14), RollupDays: intPtr(120)})
	if err != nil {
		t.Fatalf("advanced pair PUT should resolve: %v", err)
	}
	if next.RawDays != 14 || next.RollupDays != 120 {
		t.Fatalf("advanced pair should preserve explicit values, got %+v", next)
	}
}

func TestResolveRetentionPUTRejectsMixedFields(t *testing.T) {
	stored := Config{RawDays: 30, RollupDays: 90}
	cases := []retentionBody{
		{RetentionDays: intPtr(90), RawDays: intPtr(30)},
		{RetentionDays: intPtr(90), RollupDays: intPtr(90)},
		{RetentionDays: intPtr(90), RawDays: intPtr(30), RollupDays: intPtr(90)},
	}
	for i, req := range cases {
		if _, err := resolveRetentionPUT(stored, req); err == nil {
			t.Fatalf("case %d (%+v) should be rejected", i, req)
		}
	}
}

func TestResolveRetentionPUTAbsentKeepsStored(t *testing.T) {
	stored := Config{RawDays: 30, RollupDays: 120}
	next, err := resolveRetentionPUT(stored, retentionBody{})
	if err != nil {
		t.Fatalf("empty PUT should keep stored: %v", err)
	}
	if next != stored {
		t.Fatalf("empty PUT should keep stored %+v, got %+v", stored, next)
	}
}

func TestPutBoundsRejected(t *testing.T) {
	for _, c := range []Config{
		{RawDays: 0, RollupDays: 90},
		{RawDays: 30, RollupDays: 1826},
		{RawDays: 1826, RollupDays: 1826},
		{RawDays: 31, RollupDays: 90}, // raw above MaxRawDays
		{RawDays: 90, RollupDays: 30}, // rollup < raw
	} {
		if err := ValidateConfig(c); err == nil {
			t.Fatalf("%+v should be rejected", c)
		}
	}
}

func TestSingleKnobErrorNamesRetentionDays(t *testing.T) {
	// A single-knob PUT {retentionDays: N} maps onto rollupDays, so a
	// range rejection must name retentionDays, not the derived legacy
	// field the caller never sent.
	err := singleKnobError(ValidateConfig(Config{RawDays: 0, RollupDays: 0}))
	if err == nil || err.Error() != "invalid retentionDays: must be 1..1825." {
		t.Fatalf("single-knob zero = %v, want retentionDays wording", err)
	}
	err = singleKnobError(ValidateConfig(Config{RawDays: 30, RollupDays: 1826}))
	if err == nil || err.Error() != "invalid retentionDays: must be 1..1825." {
		t.Fatalf("single-knob 1826 = %v, want retentionDays wording", err)
	}
	// Non-configError values pass through untouched.
	if got := singleKnobError(nil); got != nil {
		t.Fatalf("nil should pass through, got %v", got)
	}
}
