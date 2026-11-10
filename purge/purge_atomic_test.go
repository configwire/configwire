package purge

import (
	"errors"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// TestPurgeDeleteFailureKeepsRollupsAndRawRows pins the new crash
// boundary: the day's rollup commits before the raw delete transaction, so
// a failed delete rolls back only the deletes — raw rows survive AND the
// absolute-count bucket stays on disk. A rerun sees the existing bucket,
// skips the rollup, and deletes the rows without double-counting.
func TestPurgeDeleteFailureKeepsRollupsAndRawRows(t *testing.T) {
	app := parityTestApp(t)
	cutoff := time.Now().UTC()
	old := cutoff.AddDate(0, 0, -31)
	seedEvent(t, app, "dev", "fetch", "", old, true)
	seedEvent(t, app, "dev", "exposure", "control", old, true)
	seedEvent(t, app, "dev", "fetch", "", old, true)

	failDelete := true
	app.OnRecordDelete("events").BindFunc(func(e *core.RecordEvent) error {
		if failDelete {
			return errors.New("purge-tx-test: forced delete failure")
		}
		return e.Next()
	})

	if _, err := PurgeOlderThan(app, cutoff); err == nil {
		t.Fatal("purge with failing delete succeeded, want error")
	}
	if got := len(liveIDs(t, app, "events")); got != 3 {
		t.Fatalf("after failed purge: %d raw events, want all 3 kept", got)
	}
	rows := mustAllDaily(t, app)
	if len(rows) != 2 {
		t.Fatalf("after failed purge: %d rollup rows, want 2 committed buckets (fetch + exposure)", len(rows))
	}
	var fetches, exposures int
	for _, r := range rows {
		fetches += r.GetInt("fetches")
		exposures += r.GetInt("exposures")
	}
	if fetches != 2 || exposures != 1 {
		t.Fatalf("committed bucket counts = %d/%d, want 2/1", fetches, exposures)
	}

	failDelete = false
	deleted, err := PurgeOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("rerun PurgeOlderThan: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("rerun deleted = %d, want 3", deleted)
	}
	rows = mustAllDaily(t, app)
	fetches, exposures = 0, 0
	for _, r := range rows {
		fetches += r.GetInt("fetches")
		exposures += r.GetInt("exposures")
	}
	if len(rows) != 2 || fetches != 2 || exposures != 1 {
		t.Fatalf("rerun rollups = %d rows %d/%d, want the same 2 buckets 2/1 (no double-add)", len(rows), fetches, exposures)
	}
}

// TestDailyTickerBootRunAndShutdown pins the ticker lifecycle: the boot
// run purges stale rows immediately (no 24h wait for upgraded DBs), and
// closing stop ends the goroutine.
func TestDailyTickerBootRunAndShutdown(t *testing.T) {
	app := parityTestApp(t)
	old := time.Now().UTC().AddDate(0, 0, -31)
	seedEvent(t, app, "dev", "fetch", "", old, true)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { dailyTicker(app, stop); close(done) }()

	deadline := time.Now().Add(10 * time.Second)
	for len(liveIDs(t, app, "events")) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("boot run did not purge the stale event")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ticker did not stop after close")
	}
}

// TestPurgeEnabledGate pins the multi-process opt-out: off/0/false (any
// case, padded) disable the ticker; unset and anything else enable it.
func TestPurgeEnabledGate(t *testing.T) {
	for value, want := range map[string]bool{
		"off": false, "OFF": false, " 0 ": false, "false": false, "False": false,
		"": true, "1": true, "on": true, "yes": true,
	} {
		t.Setenv("CONFIGWIRE_PURGE", value)
		if got := purgeEnabled(); got != want {
			t.Errorf("CONFIGWIRE_PURGE=%q: enabled=%v, want %v", value, got, want)
		}
	}
}

// TestPurgeIntervalJitterBounds pins the daily schedule to 23-25h so
// restarts do not phase-lock the run time.
func TestPurgeIntervalJitterBounds(t *testing.T) {
	for i := 0; i < 200; i++ {
		if d := purgeInterval(); d < 23*time.Hour || d > 25*time.Hour {
			t.Fatalf("purgeInterval = %v, want within 23h..25h", d)
		}
	}
}
