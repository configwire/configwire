package purge

import (
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// TestPurgeFailureRollsBackEverything pins purge atomicity: when a raw
// delete fails mid-run, the whole purge rolls back — no partial upserts
// land in event_daily and no raw rows are deleted. Under the old
// per-row loop this same failure left counted-but-undeleted rows that
// the next run counted again.
func TestPurgeFailureRollsBackEverything(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Now().UTC()
	old := cutoff.AddDate(0, 0, -31)
	seedEvent(t, app, "dev", "fetch", "", old, true)
	seedEvent(t, app, "dev", "exposure", "control", old, true)
	seedEvent(t, app, "dev", "fetch", "", old, true)

	app.OnRecordDelete("events").BindFunc(func(e *core.RecordEvent) error {
		return errors.New("purge-tx-test: forced delete failure")
	})

	if _, err := PurgeOlderThan(app, cutoff); err == nil {
		t.Fatal("purge with failing delete succeeded, want error")
	}
	if got := len(liveIDs(t, app, "events")); got != 3 {
		t.Fatalf("after failed purge: %d raw events, want all 3 kept", got)
	}
	if got := len(liveIDs(t, app, "event_daily")); got != 0 {
		t.Fatalf("after failed purge: %d rollup rows, want 0 (upsert rolled back)", got)
	}
}
