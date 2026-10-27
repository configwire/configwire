// Identity-scrub TDD tests: 48h bucket/country nulling on events,
// independent of the configured raw window.
//
// Contract pinned here:
//   - ts strictly older than ScrubCutoff(now) loses bucket/country;
//     exactly-at-cutoff, fresher, and zero-ts rows keep them.
//   - Rows are NEVER deleted by the scrub (counts survive for rollup);
//     only bucket/country are nulled.
//   - Dry (CountScrubOlderThan) == live (ScrubIdentitiesOlderThan) exactly
//     on a quiescent table: scrubbing creates no rows, so unlike the purge
//     path there is no same-run stale-bucket delta.
//   - Reruns converge to 0 (already-clean rows are not recounted).
package purge

import (
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// scrubTestApp mirrors parityTestApp but carries the identity columns the
// scrub touches (bucket, country).
func scrubTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	mkCol := func(name string, fields ...string) {
		t.Helper()
		col := core.NewBaseCollection(name)
		for _, f := range fields {
			if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
				t.Fatalf("add field %s to %s: %v", f, name, err)
			}
		}
		if err := app.Save(col); err != nil {
			t.Fatalf("save collection %s: %v", name, err)
		}
	}
	mkCol("events",
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"text","name":"bucket"}`,
		`{"type":"text","name":"country"}`,
		`{"type":"date","name":"ts"}`,
	)
	mkCol("event_daily",
		`{"type":"date","name":"day"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"number","name":"fetches"}`,
		`{"type":"number","name":"exposures"}`,
	)
	return app
}

func seedIdentityEvent(t *testing.T, app *tests.TestApp, ts time.Time, setTs bool, bucket, country string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", "e1")
	rec.Set("kind", "fetch")
	rec.Set("variant", "v")
	rec.Set("bucket", bucket)
	rec.Set("country", country)
	if setTs {
		rec.Set("ts", ts)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save event: %v", err)
	}
	return rec.Id
}

func getEvent(t *testing.T, app *tests.TestApp, id string) *core.Record {
	t.Helper()
	rec, err := app.FindRecordById("events", id)
	if err != nil {
		t.Fatalf("find event %s: %v", id, err)
	}
	return rec
}

// TestScrubBoundary pins the 48h rule: strictly-older dirty rows are
// nulled (rows survive), exactly-at-cutoff / fresher / zero-ts rows keep
// identity, and already-clean old rows are not recounted.
func TestScrubBoundary(t *testing.T) {
	app := scrubTestApp(t)
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	cutoff := ScrubCutoff(now) // ms-aligned (whole hours)

	idOld := seedIdentityEvent(t, app, cutoff.Add(-time.Hour), true, "abc123abc123abcd", "US")
	idCountryOnly := seedIdentityEvent(t, app, cutoff.Add(-time.Hour), true, "", "FR")
	idAt := seedIdentityEvent(t, app, cutoff, true, "abc123abc123abcd", "US")
	idFresh := seedIdentityEvent(t, app, cutoff.Add(time.Hour), true, "abc123abc123abcd", "US")
	idZero := seedIdentityEvent(t, app, time.Time{}, false, "abc123abc123abcd", "US")
	idClean := seedIdentityEvent(t, app, cutoff.Add(-time.Hour), true, "", "")

	dry, err := CountScrubOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("CountScrubOlderThan: %v", err)
	}
	if dry != 2 {
		t.Fatalf("dry scrub count = %d, want 2 (old + country-only; at/fresh/zero/clean excluded)", dry)
	}

	scrubbed, err := ScrubIdentitiesOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("ScrubIdentitiesOlderThan: %v", err)
	}
	if scrubbed != dry {
		t.Fatalf("live scrubbed = %d, dry predicted %d (want exact: no same-run delta)", scrubbed, dry)
	}

	// Nulled, not deleted: rows survive with identity gone and everything
	// else (kind, variant, ts) untouched.
	for _, id := range []string{idOld, idCountryOnly} {
		r := getEvent(t, app, id)
		if got := r.GetString("bucket"); got != "" {
			t.Errorf("event %s bucket = %q, want empty", id, got)
		}
		if got := r.GetString("country"); got != "" {
			t.Errorf("event %s country = %q, want empty", id, got)
		}
		if got := r.GetString("kind"); got != "fetch" {
			t.Errorf("event %s kind = %q, want fetch (counts grain kept)", id, got)
		}
	}
	// Kept rows still carry identity.
	for _, id := range []string{idAt, idFresh, idZero, idClean} {
		r := getEvent(t, app, id)
		if id == idClean {
			continue // was already clean; nothing to keep
		}
		if got := r.GetString("country"); got == "" {
			t.Errorf("event %s country nulled, want kept (boundary/fresh/zero-ts)", id)
		}
	}
	if got := getEvent(t, app, idAt).GetString("bucket"); got == "" {
		t.Errorf("at-cutoff bucket nulled, want kept (equal kept)")
	}

	// Rerun converges: everything past the cutoff is clean now.
	if rerun, err := ScrubIdentitiesOlderThan(app, cutoff); err != nil || rerun != 0 {
		t.Fatalf("rerun scrub = %d, err=%v; want 0 (converges)", rerun, err)
	}
	if n, err := CountScrubOlderThan(app, cutoff); err != nil || n != 0 {
		t.Fatalf("rerun dry = %d, err=%v; want 0", n, err)
	}
	if n := len(liveIDs(t, app, "events")); n != 6 {
		t.Fatalf("events rows = %d, want 6 (scrub never deletes)", n)
	}
}

// TestScrubDryVsLive documents the dry/live delta: exact equality, because
// scrubbing nulls in place and creates no rows (contrast the purge path,
// where just-rolled stale buckets make live == dry + #new-stale-buckets).
func TestScrubDryVsLive(t *testing.T) {
	app := scrubTestApp(t)
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	cutoff := ScrubCutoff(now)

	for i := 0; i < 10; i++ {
		seedIdentityEvent(t, app, cutoff.Add(-time.Duration(i+1)*time.Hour), true, "abc123abc123abcd", "US")
	}
	dry, err := CountScrubOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("CountScrubOlderThan: %v", err)
	}
	if dry != 10 {
		t.Fatalf("dry = %d, want 10", dry)
	}
	live, err := ScrubIdentitiesOlderThan(app, cutoff)
	if err != nil {
		t.Fatalf("ScrubIdentitiesOlderThan: %v", err)
	}
	if live != dry {
		t.Fatalf("live = %d, dry = %d; want live == dry exactly (in-place nulls, no new rows)", live, dry)
	}
}

// TestScrubIndependentOfRawWindow pins that the 48h scrub fires even when
// the configured raw window keeps the rows for years: a 5-day-old dirty
// row is scrubbed while a 5-year raw cutoff still keeps it.
func TestScrubIndependentOfRawWindow(t *testing.T) {
	app := scrubTestApp(t)
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	id := seedIdentityEvent(t, app, now.AddDate(0, 0, -5), true, "abc123abc123abcd", "US")

	rawCutoff := now.AddDate(0, 0, -1825) // rawDays=1825 keeps the 5d row
	if n, err := CountOlderThan(app, rawCutoff); err != nil || n != 0 {
		t.Fatalf("purge dry at 5yr window = %d, err=%v; want 0 (row kept)", n, err)
	}
	if m, err := CountScrubOlderThan(app, ScrubCutoff(now)); err != nil || m != 1 {
		t.Fatalf("scrub dry = %d, err=%v; want 1 (48h fires regardless)", m, err)
	}
	if m, err := ScrubIdentitiesOlderThan(app, ScrubCutoff(now)); err != nil || m != 1 {
		t.Fatalf("scrub live = %d, err=%v; want 1", m, err)
	}
	if got := getEvent(t, app, id).GetString("bucket"); got != "" {
		t.Fatalf("bucket = %q, want nulled", got)
	}
	if n := len(liveIDs(t, app, "events")); n != 1 {
		t.Fatalf("events rows = %d, want 1 (kept for the 5yr window)", n)
	}
}

// TestScrubKeepsRowsForRollup pins that scrubbed rows still roll up: rows
// 35d old (past 48h, inside the raw window) are nulled first, then the
// purge deletes them and merges their counts into event_daily.
func TestScrubKeepsRowsForRollup(t *testing.T) {
	app := scrubTestApp(t)
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	rawCutoff := now.AddDate(0, 0, -30)
	old := rawCutoff.AddDate(0, 0, -5)

	seedIdentityEvent(t, app, old, true, "abc123abc123abcd", "US")
	seedIdentityEvent(t, app, old.Add(time.Millisecond), true, "abc123abc123abcd", "US")

	if m, err := ScrubIdentitiesOlderThan(app, ScrubCutoff(now)); err != nil || m != 2 {
		t.Fatalf("scrub = %d, err=%v; want 2", m, err)
	}
	n, err := PurgeOlderThan(app, rawCutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 2 {
		t.Fatalf("purge deleted = %d, want 2 (scrubbed rows still roll + delete)", n)
	}
	rows := mustAllDaily(t, app)
	if len(rows) != 1 {
		t.Fatalf("event_daily rows = %d, want 1 merged bucket", len(rows))
	}
	if got := rows[0].GetInt("fetches"); got != 2 {
		t.Fatalf("merged fetches = %d, want 2", got)
	}
}
