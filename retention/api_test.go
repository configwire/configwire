package retention

import (
	"testing"
)

func intPtr(n int) *int { return &n }

func TestToBodyShape(t *testing.T) {
	body := toBody(Config{RawDays: 45, RollupDays: 120})
	if body["rawDays"] != 45 || body["rollupDays"] != 120 {
		t.Fatalf("unexpected body shape: %+v", body)
	}
}

func TestResolvePUTDaysNilKeepsStored(t *testing.T) {
	if got := resolvePUTDays(30, nil); got != 30 {
		t.Fatalf("nil should keep stored 30, got %d", got)
	}
	if got := resolvePUTDays(30, intPtr(45)); got != 45 {
		t.Fatalf("explicit 45 should win, got %d", got)
	}
}

func TestPutMergeSemantics(t *testing.T) {
	resetForTest(Config{RawDays: 30, RollupDays: 90})
	stored := GetConfig()
	// Absent rawDays keeps stored; explicit rollupDays replaces.
	next := Config{
		RawDays:    resolvePUTDays(stored.RawDays, nil),
		RollupDays: resolvePUTDays(stored.RollupDays, intPtr(120)),
	}
	if next.RawDays != 30 || next.RollupDays != 120 {
		t.Fatalf("merge should be {30,120}, got %+v", next)
	}
	if err := ValidateConfig(next); err != nil {
		t.Fatalf("merged config should validate: %v", err)
	}
	// Both absent keeps everything.
	keep := Config{
		RawDays:    resolvePUTDays(stored.RawDays, nil),
		RollupDays: resolvePUTDays(stored.RollupDays, nil),
	}
	if keep != stored {
		t.Fatalf("all-absent merge should keep stored, got %+v", keep)
	}
	// Invariant-violating explicit pair is rejected.
	bad := Config{
		RawDays:    resolvePUTDays(stored.RawDays, intPtr(100)),
		RollupDays: resolvePUTDays(stored.RollupDays, intPtr(50)),
	}
	if err := ValidateConfig(bad); err == nil {
		t.Fatalf("rollup<raw merge should be rejected: %+v", bad)
	}
	resetForTest(Defaults())
}

func TestPutBoundsRejected(t *testing.T) {
	for _, c := range []Config{
		{RawDays: 0, RollupDays: 90},
		{RawDays: 30, RollupDays: 1826},
		{RawDays: 1826, RollupDays: 1826},
	} {
		if err := ValidateConfig(c); err == nil {
			t.Fatalf("%+v should be rejected", c)
		}
	}
}
