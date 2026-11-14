package purge

import (
	"testing"
	"time"

	"github.com/configwire/configwire/retention"
)

// TestRawCutoffIsDayGranular pins the purge cutoff shape: the UTC midnight
// of now-rawDays, so whole UTC days are deleted and the boundary day stays
// fully raw-owned (stats reads it from raw events until it leaves the
// window entirely; see rawCutoff).
func TestRawCutoffIsDayGranular(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 42, 7, 123, time.UTC)
	got := RawCutoff(now)
	want := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("rawCutoff(%v) = %v, want %v", now, got, want)
	}

	// Short retention: midnight of now-EffectiveRawDays.
	retention.ResetForTest(retention.Config{RawDays: 7, RollupDays: 7})
	t.Cleanup(func() { retention.ResetForTest(retention.Defaults()) })
	got = RawCutoff(now)
	want = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("short-retention rawCutoff(%v) = %v, want %v", now, got, want)
	}
}
