package stats

import (
	"testing"
	"time"

	"github.com/configwire/configwire/purge"
)

// TestStatsCountsBoundaryDayAfterPurge pins the whole-day tiling between
// purge, rollups, and stats reads around the raw-window boundary. The raw
// cutoff is day-granular (purge.rawCutoff: the UTC midnight of
// now-rawDays), so the boundary day (exactly rawDays ago) is fully
// raw-owned: it is never rolled and never purged, and a >30d window reads
// it from raw events. The day before the boundary IS rolled by
// EnsureRollups and purged, and stats reads it back from its event_daily
// bucket. With an instant-based cutoff the boundary day was split at a
// sub-day grain: its early rows were purged into a whole-day bucket the
// rollup read (day < horizon) never sees, silently undercounting long
// windows.
func TestStatsCountsBoundaryDayAfterPurge(t *testing.T) {
	f := newRollupStatsApp(t)
	now := time.Now().UTC()
	boundaryDay := purge.DayBucket(now.AddDate(0, 0, -30))
	previousDay := boundaryDay.AddDate(0, 0, -1)

	// Rolled + purged side: the day before the boundary.
	f.seedRaw(t, "", "fetch", "", 1, previousDay.Add(12*time.Hour))
	// Raw-owned side: early hours of the boundary day (survives the
	// midnight cutoff no matter what time of day this test runs).
	f.seedRaw(t, "", "fetch", "", 1, boundaryDay.Add(30*time.Minute))
	// Uncontroversial raw event well inside the raw window.
	f.seedRaw(t, "", "fetch", "", 1, now.Add(-2*time.Hour))

	// Simulate the daily ticker run with its exact cutoff formula
	// (purge.RawCutoff), so a regression in the cutoff shape fails here.
	cutoff := purge.RawCutoff(now)
	if _, err := purge.EnsureRollups(f.app, now); err != nil {
		t.Fatalf("EnsureRollups: %v", err)
	}
	if _, err := purge.PurgeOlderThan(f.app, cutoff); err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}

	code, body := callStats(t, f.app, "?since=90d")
	if code != 200 {
		t.Fatalf("getStats(?since=90d) status = %d, want 200", code)
	}
	if got := numField(t, body, "fetches"); got != 3 {
		t.Errorf("fetches = %v, want 3 (boundary-day early rows must survive purge and count)", got)
	}
	events, rollups := sourcesOf(t, body)
	if events != 2 || rollups != 1 {
		t.Errorf("sources = %v/%v, want 2 events (raw side) / 1 rollup (purged previous day)", events, rollups)
	}
	sf, se := seriesSums(t, body)
	if sf != 3 || se != 0 {
		t.Errorf("series sums = %d/%d, want 3/0 (series must tile the window with the totals)", sf, se)
	}
}
