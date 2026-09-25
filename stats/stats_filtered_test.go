// Filtered-query parity + index-proof tests for the stats hot path.
//
// The loadRows/loadRollups loaders must query by indexed filter
// (env[, flag], ts/day range) instead of full-scanning. These tests pin:
//   - TestStatsParityNoScan: no FindAllRecords on the hot path (RED
//     pre-fix when the scan was present, GREEN post-fix).
//   - TestStatsParityRows / TestStatsParityRollups: on seeded mixed
//     env/flag rows, the new filtered queries aggregated through the
//     FROZEN pure functions equal the old full-scan logic byte-for-byte
//     (diff-test oracle: FindAllRecords + project + Aggregate).
//   - TestExplainStatsIndexes: EXPLAIN QUERY PLAN proves the on-disk
//     scale indexes serve the exact filter shapes.
package stats

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/configwire/configwire/purge"
)

// TestStatsParityNoScan gates the hot path: the loadRows/loadRollups bodies
// in stats.go must query by indexed filter (FindRecordsByFilter) and must
// not full-scan (FindAllRecords).
func TestStatsParityNoScan(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(".", "stats.go"))
	if err != nil {
		t.Fatalf("read stats.go: %v", err)
	}
	for _, fn := range []string{"func loadRows(", "func loadRollups("} {
		body := extractFuncBody(t, string(src), fn)
		if strings.Contains(body, "FindAllRecords") {
			t.Errorf("%s must not full-scan via FindAllRecords; body:\n%s", fn, body)
		}
		if !strings.Contains(body, "FindRecordsByFilter") {
			t.Errorf("%s must use an indexed FindRecordsByFilter query; body:\n%s", fn, body)
		}
	}
}

// extractFuncBody returns the source of the func starting at prefix
// through its closing brace at column 0.
func extractFuncBody(t *testing.T, src, prefix string) string {
	t.Helper()
	start := strings.Index(src, prefix)
	if start < 0 {
		t.Fatalf("%s not found in stats.go", prefix)
	}
	rest := src[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("could not find end of %s in stats.go", prefix)
	}
	return rest[:end+3]
}

// statsFilteredTestApp builds a TestApp with events + event_daily
// collections carrying the exact on-disk scale index names from migration
// 1790000004 (env/flag/ts + day shapes). Relation targets are plain text
// holding the env/flag ids: the loaders only read them via GetString,
// exactly like the handler path.
func statsFilteredTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	events := core.NewBaseCollection("events")
	for _, f := range []string{
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"date","name":"ts"}`,
	} {
		if err := events.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add events field %s: %v", f, err)
		}
	}
	events.AddIndex("idx_events_env_ts", false, "env, ts", "")
	events.AddIndex("idx_events_env_flag_ts", false, "env, flag, ts", "")
	if err := app.Save(events); err != nil {
		t.Fatalf("save events collection: %v", err)
	}

	daily := core.NewBaseCollection("event_daily")
	for _, f := range []string{
		`{"type":"date","name":"day"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"fetches"}`,
		`{"type":"number","name":"exposures"}`,
	} {
		if err := daily.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add event_daily field %s: %v", f, err)
		}
	}
	daily.AddIndex("idx_event_daily_upsert_key", true, "day, env, flag, variant", "")
	daily.AddIndex("idx_event_daily_day", false, "day", "")
	if err := app.Save(daily); err != nil {
		t.Fatalf("save event_daily collection: %v", err)
	}
	return app
}

func seedEvent(t *testing.T, app *tests.TestApp, env, flag, kind, variant string, ts time.Time) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", env)
	rec.Set("flag", flag)
	rec.Set("kind", kind)
	rec.Set("variant", variant)
	rec.Set("ts", ts)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save event: %v", err)
	}
}

func seedBucket(t *testing.T, app *tests.TestApp, day time.Time, env, flag, variant string, fetches, exposures int) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		t.Fatalf("find event_daily: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("day", day)
	rec.Set("env", env)
	rec.Set("flag", flag)
	rec.Set("variant", variant)
	rec.Set("fetches", fetches)
	rec.Set("exposures", exposures)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save bucket: %v", err)
	}
}

// seedMixedStats writes mixed env/flag rows around one fixed now0 and
// returns it so cutoffs stay put between seeding and assertion.
func seedMixedStats(t *testing.T, app *tests.TestApp) time.Time {
	t.Helper()
	now0 := time.Now().UTC()
	horizon0 := HorizonFor(now0)
	cutoff90 := now0.Add(-90 * 24 * time.Hour)
	eventCutoff7 := EventCutoffFor(now0.Add(-7*24*time.Hour), horizon0)

	// Events: e1/f1 core, e1/f2 + unset-relation siblings, e2 other-env,
	// 100d stale pair, and the 7d-window boundary pair.
	seedEvent(t, app, "e1", "f1", "fetch", "", now0)
	seedEvent(t, app, "e1", "f1", "fetch", "", now0)
	seedEvent(t, app, "e1", "f1", "exposure", "control", now0)
	seedEvent(t, app, "e1", "f1", "exposure", "control", now0)
	seedEvent(t, app, "e1", "f1", "exposure", "treatment", now0)
	seedEvent(t, app, "e1", "f2", "fetch", "", now0)
	seedEvent(t, app, "e1", "f2", "exposure", "control", now0)
	seedEvent(t, app, "e1", "", "fetch", "", now0)
	seedEvent(t, app, "e1", "", "exposure", "", now0)
	seedEvent(t, app, "e2", "f1", "fetch", "", now0)
	seedEvent(t, app, "e2", "f1", "exposure", "control", now0)
	seedEvent(t, app, "e1", "f1", "fetch", "", now0.Add(-100*24*time.Hour))
	seedEvent(t, app, "e1", "f1", "exposure", "control", now0.Add(-100*24*time.Hour))
	seedEvent(t, app, "e1", "f1", "fetch", "", eventCutoff7)
	seedEvent(t, app, "e1", "f1", "fetch", "", eventCutoff7.Add(-time.Second))

	// Rollups: history pair, other-flag/other-env/unset siblings,
	// exactly-at-90d-cutoff kept, stale pair, raw-side boundary day.
	cutoffDay90 := purge.DayBucket(cutoff90)
	seedBucket(t, app, horizon0.AddDate(0, 0, -5), "e1", "f1", "control", 7, 9)
	seedBucket(t, app, horizon0.AddDate(0, 0, -5), "e1", "f1", "treatment", 1, 2)
	seedBucket(t, app, horizon0.AddDate(0, 0, -5), "e1", "f2", "control", 4, 5)
	seedBucket(t, app, horizon0.AddDate(0, 0, -5), "e2", "f1", "control", 6, 7)
	seedBucket(t, app, horizon0.AddDate(0, 0, -5), "e1", "", "", 2, 3)
	seedBucket(t, app, cutoffDay90, "e1", "f1", "control", 3, 4)
	seedBucket(t, app, cutoffDay90.AddDate(0, 0, -1), "e1", "f1", "control", 50, 60)
	seedBucket(t, app, horizon0, "e1", "f1", "control", 11, 13)
	seedBucket(t, app, horizon0.AddDate(0, 0, -100), "e1", "f1", "control", 21, 22)
	return now0
}

// oldEventRows is the pre-fix oracle: full scan + project, filtered only
// by the frozen Aggregate afterwards.
func oldEventRows(t *testing.T, app *tests.TestApp) []EventRow {
	t.Helper()
	recs, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatalf("FindAllRecords(events): %v", err)
	}
	rows := make([]EventRow, 0, len(recs))
	for _, r := range recs {
		rows = append(rows, EventRow{
			EnvID:   r.GetString("env"),
			FlagID:  r.GetString("flag"),
			Kind:    r.GetString("kind"),
			Variant: r.GetString("variant"),
			Ts:      r.GetDateTime("ts").Time(),
		})
	}
	return rows
}

// oldRollups is the pre-fix oracle for event_daily.
func oldRollups(t *testing.T, app *tests.TestApp) []purge.Rollup {
	t.Helper()
	recs, err := app.FindAllRecords("event_daily")
	if err != nil {
		t.Fatalf("FindAllRecords(event_daily): %v", err)
	}
	out := make([]purge.Rollup, 0, len(recs))
	for _, r := range recs {
		out = append(out, purge.Rollup{
			Day:       r.GetDateTime("day").Time(),
			EnvID:     r.GetString("env"),
			FlagID:    r.GetString("flag"),
			Variant:   r.GetString("variant"),
			Fetches:   r.GetInt("fetches"),
			Exposures: r.GetInt("exposures"),
		})
	}
	return out
}

type flagCase struct {
	name       string
	flagID     string
	filterFlag bool
}

var parityFlagCases = []flagCase{
	{"unfiltered", "", false},
	{"flag f1", "f1", true},
	{"unknown flag", "no-such-flag", true},
	// Exact-mirror edge: an unmatched ?flag= resolves to flagID "" with
	// the filter on, which matches unset-relation rows in BOTH paths.
	{"unset-relation filter", "", true},
}

// TestStatsParityRows diff-tests the new indexed loadRows against the old
// full-scan logic: same frozen Aggregate over both must agree exactly, in
// the 7d events-only window and the 90d merged window.
func TestStatsParityRows(t *testing.T) {
	app := statsFilteredTestApp(t)
	now0 := seedMixedStats(t, app)
	oracle := oldEventRows(t, app)

	for _, days := range []int{7, 90} {
		cutoff := now0.Add(-time.Duration(days) * 24 * time.Hour)
		horizon := HorizonFor(now0)
		eventCutoff := EventCutoffFor(cutoff, horizon)
		for _, tc := range parityFlagCases {
			want := Aggregate(oracle, "e1", tc.flagID, tc.filterFlag, eventCutoff)
			got, err := loadRows(app, "e1", tc.flagID, tc.filterFlag, eventCutoff)
			if err != nil {
				t.Fatalf("%dd %s: loadRows: %v", days, tc.name, err)
			}
			if gotAgg := Aggregate(got, "e1", tc.flagID, tc.filterFlag, eventCutoff); !reflect.DeepEqual(gotAgg, want) {
				t.Errorf("%dd %s: filtered Aggregate = %+v, want scan-oracle %+v", days, tc.name, gotAgg, want)
			}
		}
	}

	// The filter must bite: unfiltered 7d over e1 keeps only live e1 rows,
	// strictly fewer than the seeded scan (other-env + stale excluded).
	eventCutoff7 := EventCutoffFor(now0.Add(-7*24*time.Hour), HorizonFor(now0))
	live, err := loadRows(app, "e1", "", false, eventCutoff7)
	if err != nil {
		t.Fatalf("loadRows unfiltered 7d: %v", err)
	}
	if len(live) >= len(oracle) {
		t.Errorf("filtered 7d returned %d rows over %d seeded; filter excluded nothing", len(live), len(oracle))
	}
	for _, r := range live {
		if r.EnvID != "e1" {
			t.Errorf("filtered 7d leaked other-env row %+v", r)
		}
		// Millisecond grain: stored ts truncates to DefaultDateLayout, so
		// a row within 1ms below the cutoff is the documented superset
		// slop the frozen Aggregate narrows back down — anything older
		// is a real leak.
		if r.Ts.Before(eventCutoff7.Add(-time.Millisecond)) {
			t.Errorf("filtered 7d leaked pre-cutoff row %+v", r)
		}
	}
}

// TestStatsParityRollups diff-tests the new indexed loadRollups against the
// old full-scan logic, and pins the merged total both paths agree on.
func TestStatsParityRollups(t *testing.T) {
	app := statsFilteredTestApp(t)
	now0 := seedMixedStats(t, app)
	oracle := oldRollups(t, app)
	oracleEvents := oldEventRows(t, app)

	for _, days := range []int{7, 90} {
		cutoff := now0.Add(-time.Duration(days) * 24 * time.Hour)
		horizon := HorizonFor(now0)
		cutoffDay := purge.DayBucket(cutoff)
		eventCutoff := EventCutoffFor(cutoff, horizon)
		for _, tc := range parityFlagCases {
			want := AggregateRollups(oracle, "e1", tc.flagID, tc.filterFlag, cutoffDay, horizon)
			got, err := loadRollups(app, "e1", tc.flagID, tc.filterFlag, cutoffDay, horizon)
			if err != nil {
				t.Fatalf("%dd %s: loadRollups: %v", days, tc.name, err)
			}
			if gotAgg := AggregateRollups(got, "e1", tc.flagID, tc.filterFlag, cutoffDay, horizon); !reflect.DeepEqual(gotAgg, want) {
				t.Errorf("%dd %s: filtered AggregateRollups = %+v, want scan-oracle %+v", days, tc.name, gotAgg, want)
			}

			// Merged end-to-end parity for the same window+flag.
			wantEvents := Aggregate(oracleEvents, "e1", tc.flagID, tc.filterFlag, eventCutoff)
			gotEvents, err := loadRows(app, "e1", tc.flagID, tc.filterFlag, eventCutoff)
			if err != nil {
				t.Fatalf("%dd %s: loadRows: %v", days, tc.name, err)
			}
			wantMerged := MergeStats(wantEvents, want)
			gotMerged := MergeStats(
				Aggregate(gotEvents, "e1", tc.flagID, tc.filterFlag, eventCutoff),
				AggregateRollups(got, "e1", tc.flagID, tc.filterFlag, cutoffDay, horizon),
			)
			if !reflect.DeepEqual(gotMerged, wantMerged) {
				t.Errorf("%dd %s: filtered merged = %+v, want scan-oracle %+v", days, tc.name, gotMerged, wantMerged)
			}
		}
	}

	// Spot value: 90d flag-f1 merges the history buckets with the
	// at-cutoff bucket; stale/raw-side/other excluded.
	cutoff90 := now0.Add(-90 * 24 * time.Hour)
	horizon := HorizonFor(now0)
	buckets, err := loadRollups(app, "e1", "f1", true, purge.DayBucket(cutoff90), horizon)
	if err != nil {
		t.Fatalf("loadRollups 90d f1: %v", err)
	}
	st := AggregateRollups(buckets, "e1", "f1", true, purge.DayBucket(cutoff90), horizon)
	// history (7,9) + treatment (1,2) + at-cutoff (3,4).
	if st.Fetches != 11 || st.Exposures != 15 {
		t.Errorf("90d f1 rollups = %+v, want {Fetches:11 Exposures:15}", st)
	}
}

// explainDetails runs EXPLAIN QUERY PLAN, logs every line (evidence), and
// returns the plan details.
func explainDetails(t *testing.T, app *tests.TestApp, sql string) []string {
	t.Helper()
	rows, err := app.ConcurrentDB().NewQuery(sql).Rows()
	if err != nil {
		t.Fatalf("EXPLAIN %q: %v", sql, err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		details = append(details, detail)
		t.Logf("EXPLAIN: %s", detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN rows: %v", err)
	}
	return details
}

func stamp(t time.Time) string {
	return t.UTC().Format(types.DefaultDateLayout)
}

// TestExplainStatsIndexes proves the on-disk scale indexes serve the exact
// filter shapes the loaders emit — no full-table scan at any window.
func TestExplainStatsIndexes(t *testing.T) {
	app := statsFilteredTestApp(t)
	now0 := seedMixedStats(t, app)
	eventCutoff := EventCutoffFor(now0.Add(-7*24*time.Hour), HorizonFor(now0))
	cutoffDay := purge.DayBucket(now0.Add(-90 * 24 * time.Hour))
	horizon := HorizonFor(now0)

	join := func(details []string) string { return strings.Join(details, "\n") }

	flagged := explainDetails(t, app,
		`EXPLAIN QUERY PLAN SELECT * FROM "events" WHERE "env" = 'e1' AND "flag" = 'f1' AND "ts" >= '`+stamp(eventCutoff)+`'`)
	if !strings.Contains(join(flagged), "idx_events_env_flag_ts") {
		t.Errorf("flag-filtered events plan misses idx_events_env_flag_ts:\n%s", join(flagged))
	}

	unfiltered := explainDetails(t, app,
		`EXPLAIN QUERY PLAN SELECT * FROM "events" WHERE "env" = 'e1' AND "ts" >= '`+stamp(eventCutoff)+`'`)
	if !strings.Contains(join(unfiltered), "idx_events_env_ts") {
		t.Errorf("unfiltered events plan misses idx_events_env_ts:\n%s", join(unfiltered))
	}

	daily := explainDetails(t, app,
		`EXPLAIN QUERY PLAN SELECT * FROM "event_daily" WHERE "env" = 'e1' AND "flag" = 'f1' AND "day" >= '`+stamp(cutoffDay)+`' AND "day" < '`+stamp(horizon)+`'`)
	if !strings.Contains(join(daily), "idx_event_daily") {
		t.Errorf("event_daily plan misses idx_event_daily_*:\n%s", join(daily))
	}
	for _, d := range daily {
		if strings.Contains(d, "SCAN") && !strings.Contains(d, "USING") {
			t.Errorf("event_daily plan falls back to full scan:\n%s", join(daily))
		}
	}
}
