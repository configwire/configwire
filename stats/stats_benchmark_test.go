// Benchmark for the stats rollup read path: one seeded database with ~1M
// raw events plus 90 days of event_daily, measured at the handler's actual
// auto-mode window (today raw + completed days rolled up).
package stats

import (
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"

	"github.com/configwire/configwire/purge"
)

// benchStatsApp builds the collections/indexes the stats loaders query.
func benchStatsApp(b *testing.B) *tests.TestApp {
	b.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		b.Fatalf("NewTestApp: %v", err)
	}
	b.Cleanup(func() { app.Cleanup() })

	mkCol := func(name string, fields []string, indexes [][3]string) *core.Collection {
		b.Helper()
		col := core.NewBaseCollection(name)
		for _, f := range fields {
			if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
				b.Fatalf("add field %s to %s: %v", f, name, err)
			}
		}
		for _, idx := range indexes {
			col.AddIndex(idx[0], false, idx[1], "")
		}
		if err := app.Save(col); err != nil {
			b.Fatalf("save collection %s: %v", name, err)
		}
		return col
	}
	mkCol("events", []string{
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"date","name":"ts"}`,
	}, [][3]string{
		{"idx_events_env_ts", "env, ts", ""},
	})
	mkCol("event_daily", []string{
		`{"type":"date","name":"day"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"number","name":"fetches"}`,
		`{"type":"number","name":"exposures"}`,
	}, [][3]string{
		{"idx_event_daily_env_day", "env, day", ""},
		{"idx_event_daily_day", "day", ""},
	})
	return app
}

// BenchmarkStatsRollupRead seeds ~1M raw events (81 days of seconds-spaced
// rows) and 90 days x 10 rollup buckets once, then measures the auto-mode
// read: today's raw slice + completed-day rollups + merge + series.
//
// The row-count assertion is the regression gate (no wall-clock gate): a
// read that fell back to the old now-RawDays raw window would return
// ~333k rows and fail here.
//
// Measured 2026-11-10 (go1.27.2, Apple M1 Pro), -benchtime=20x -count=3:
// 10.20/10.65/10.23 ms/op, p95 ≈ 10.65 ms/op, ~6.26 MB/op,
// ~133.3k allocs/op. Allocations are dominated by the today raw slice
// (~11k rows) and scale with events today, not with the 90-day history.
func BenchmarkStatsRollupRead(b *testing.B) {
	app := benchStatsApp(b)
	now := time.Now().UTC()
	today := purge.DayBucket(now)
	cutoffDay := today.AddDate(0, 0, -90)

	if _, err := app.DB().NewQuery(`
WITH RECURSIVE seq(n) AS (
  SELECT 1
  UNION ALL
  SELECT n + 1 FROM seq WHERE n < ` + "1000000" + `
)
INSERT INTO ` + "`events`" + ` (id, env, flag, kind, variant, version, ts)
SELECT printf('b%014d', n), 'e1', '', 'fetch', '', 1,
       strftime('%Y-%m-%d %H:%M:%f', 'now', printf('-%d seconds', n * 7)) || 'Z'
FROM seq`).Execute(); err != nil {
		b.Fatalf("seed raw events: %v", err)
	}
	daily, err := app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		b.Fatalf("find event_daily: %v", err)
	}
	if err := app.RunInTransaction(func(txApp core.App) error {
		for d := 1; d <= 90; d++ {
			for v := 0; v < 10; v++ {
				rec := core.NewRecord(daily)
				rec.Set("day", cutoffDay.AddDate(0, 0, d))
				rec.Set("env", "e1")
				rec.Set("variant", "v")
				rec.Set("version", v)
				rec.Set("fetches", 10)
				rec.Set("exposures", 20)
				if err := txApp.Save(rec); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		b.Fatalf("seed event_daily: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := loadRows(app, "e1", today)
		if err != nil {
			b.Fatalf("loadRows: %v", err)
		}
		if len(rows) > 50000 {
			b.Fatalf("raw read returned %d rows; auto mode must read only today's raw slice", len(rows))
		}
		buckets, err := loadRollups(app, "e1", cutoffDay, today)
		if err != nil {
			b.Fatalf("loadRollups: %v", err)
		}
		if len(buckets) < 800 {
			b.Fatalf("loaded %d rollup buckets, want the seeded 90-day history", len(buckets))
		}
		st := MergeStats(
			Aggregate(rows, "e1", today),
			AggregateRollups(buckets, "e1", cutoffDay, today),
		)
		series := MergeSeries(
			BuildEventSeries(rows, "e1", today),
			BuildRollupSeries(buckets, "e1", cutoffDay, today),
			SeriesDays(cutoffDay, now),
		)
		if TotalFor(st) == 0 || len(series) < 90 {
			b.Fatalf("empty stats result: total=%d series=%d", TotalFor(st), len(series))
		}
	}
}
