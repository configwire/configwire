package stats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/configwire/configwire/purge"
	"github.com/configwire/configwire/retention"
)

func TestHourlyForGate(t *testing.T) {
	cases := []struct {
		days, raw int
		want      bool
	}{
		{1, 1, true},
		{2, 2, true},
		{1, 90, true},
		{2, 1825, true},
		{7, 30, false},
		{7, 90, false},
		{3, 90, false},
		{0, 30, false},
		{1, 0, false},
		{2, 1, false}, // 2d needs at least 2d raw retention
	}
	for _, c := range cases {
		if got := HourlyFor(c.days, c.raw); got != c.want {
			t.Errorf("HourlyFor(%d,%d) = %v, want %v", c.days, c.raw, got, c.want)
		}
	}
	if got := GranularityFor(true); got != "hour" {
		t.Errorf("GranularityFor(true) = %q, want hour", got)
	}
	if got := GranularityFor(false); got != "day" {
		t.Errorf("GranularityFor(false) = %q, want day", got)
	}
}

func TestHourlySeriesHoursDense(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		days int
		want int
	}{
		{1, 25},
		{2, 49},
	} {
		cutoff := now.Add(-time.Duration(tc.days) * 24 * time.Hour)
		hours := HourlySeriesHours(cutoff, now)
		if len(hours) != tc.want {
			t.Fatalf("%dd: len = %d, want %d", tc.days, len(hours), tc.want)
		}
		for i, h := range hours {
			if h.Location() != time.UTC {
				t.Fatalf("%dd hour %d loc = %v, want UTC", tc.days, i, h.Location())
			}
			if h.Minute() != 0 || h.Second() != 0 || h.Nanosecond() != 0 {
				t.Fatalf("%dd hour %d = %v, want hour-aligned", tc.days, i, h)
			}
			if i > 0 && !hours[i].Equal(hours[i-1].Add(time.Hour)) {
				t.Fatalf("%dd hour %d = %v, want +1h after %v", tc.days, i, hours[i], hours[i-1])
			}
		}
		if !hours[0].Equal(HourBucket(cutoff)) {
			t.Errorf("%dd first = %v, want %v", tc.days, hours[0], HourBucket(cutoff))
		}
		if !hours[len(hours)-1].Equal(HourBucket(now)) {
			t.Errorf("%dd last = %v, want %v", tc.days, hours[len(hours)-1], HourBucket(now))
		}
	}
	if HourlySeriesHours(time.Time{}, now) != nil {
		t.Errorf("zero cutoff should yield nil hours")
	}
}

func TestHourlySeriesBucketing(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 4, 5, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	base := HourBucket(now).Add(-2 * time.Hour)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Version: 1, Ts: base.Add(10 * time.Minute)},
		{EnvID: "e1", Kind: "fetch", Version: 1, Ts: base.Add(50 * time.Minute)},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: base.Add(20 * time.Minute)},
		{EnvID: "e1", Kind: "weird", Ts: base.Add(30 * time.Minute)},
		{EnvID: "e1", Kind: "fetch", Ts: time.Time{}},
		{EnvID: "e1", Kind: "fetch", Ts: cutoff.Add(-time.Hour)},
		{EnvID: "e2", Kind: "fetch", Ts: base.Add(10 * time.Minute)},
	}
	got := BuildEventSeriesHourly(rows, "e1", cutoff)
	if len(got) != 1 {
		t.Fatalf("hourly map keys = %v, want single hour bucket", got)
	}
	p := got[hourKey(base)]
	if p == nil {
		t.Fatalf("missing bucket for %s", hourKey(base))
	}
	if p.Fetches != 2 || p.Exposures != 1 {
		t.Errorf("bucket = %+v, want {fetches:2 exposures:1}", p)
	}
	if p.Hour != hourKey(base) {
		t.Errorf("Hour = %q, want %q", p.Hour, hourKey(base))
	}
	if p.Day != dayKey(purge.DayBucket(base)) {
		t.Errorf("Day = %q, want day of hour %q", p.Day, hourKey(base))
	}
}

func TestMergeHourlySumsEqualAggregate(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 4, 5, 0, time.UTC)
	for _, days := range []int{1, 2} {
		cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
		cur := HourBucket(now).Add(-3 * time.Hour)
		rows := []EventRow{
			{EnvID: "e1", Kind: "fetch", Version: 1, Ts: cur.Add(5 * time.Minute)},
			{EnvID: "e1", Kind: "fetch", Version: 2, Ts: cur.Add(10 * time.Minute)},
			{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: cur.Add(15 * time.Minute)},
			{EnvID: "e1", Kind: "fetch", Version: 1, Ts: now.Add(-30 * time.Minute)},
			{EnvID: "e2", Kind: "fetch", Ts: now},
		}
		hours := HourlySeriesHours(cutoff, now)
		merged := MergeHourlySeries(BuildEventSeriesHourly(rows, "e1", cutoff), hours)
		if len(merged) != len(hours) {
			t.Fatalf("%dd: merged len = %d, want dense %d", days, len(merged), len(hours))
		}
		for i := 1; i < len(merged); i++ {
			if merged[i].Hour <= merged[i-1].Hour {
				t.Fatalf("%dd: hourly series not chronological at %d: %q after %q", days, i, merged[i].Hour, merged[i-1].Hour)
			}
			if merged[i].Day < merged[i-1].Day {
				t.Fatalf("%dd: day regressed at %d: %q after %q", days, i, merged[i].Day, merged[i-1].Day)
			}
		}
		var sf, se int
		for _, p := range merged {
			sf += p.Fetches
			se += p.Exposures
			if p.Versions == nil {
				t.Fatalf("%dd: hour %s versions is nil, want non-nil map", days, p.Hour)
			}
			if p.Hour == "" {
				t.Fatalf("%dd: hourly point missing Hour label", days)
			}
		}
		want := Aggregate(rows, "e1", cutoff)
		if sf != want.Fetches || se != want.Exposures {
			t.Errorf("%dd: hourly sums = %d/%d, want Aggregate %d/%d", days, sf, se, want.Fetches, want.Exposures)
		}
	}
}

func TestMergeHourlyDayKeyedConsumersStillSum(t *testing.T) {
	// Hourly points carry Day too, so day-keyed grouping sums back to the total.
	now := time.Date(2026, 11, 14, 15, 4, 5, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Ts: HourBucket(now).Add(-25 * time.Hour).Add(30 * time.Minute)},
		{EnvID: "e1", Kind: "fetch", Ts: HourBucket(now).Add(-1 * time.Hour).Add(10 * time.Minute)},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: HourBucket(now).Add(-1 * time.Hour).Add(20 * time.Minute)},
	}
	merged := MergeHourlySeries(BuildEventSeriesHourly(rows, "e1", cutoff), HourlySeriesHours(cutoff, now))
	byDay := map[string]int{}
	for _, p := range merged {
		byDay[p.Day] += p.Fetches + p.Exposures
	}
	total := 0
	for _, n := range byDay {
		total += n
	}
	want := TotalFor(Aggregate(rows, "e1", cutoff))
	if total != want {
		t.Errorf("day-grouped hourly total = %d, want %d", total, want)
	}
}

func TestHourlyHandlerUsesRawOnly(t *testing.T) {
	app, _ := flagIgnoranceApp(t)
	for _, q := range []string{"?since=1d", "?since=2d"} {
		code, body := callStats(t, app, q)
		if code != 200 {
			t.Fatalf("getStats(%q) status = %d, want 200", q, code)
		}
		echo, ok := body["echo"].(map[string]any)
		if !ok {
			t.Fatalf("getStats(%q) echo is not an object: %v", q, body["echo"])
		}
		if echo["granularity"] != "hour" {
			t.Errorf("getStats(%q) granularity = %v, want hour", q, echo["granularity"])
		}
		if body["approximate"] != false {
			t.Errorf("getStats(%q) approximate = %v, want false (raw-only)", q, body["approximate"])
		}
		sources, ok := body["sources"].(map[string]any)
		if !ok {
			t.Fatalf("getStats(%q) sources is not an object: %v", q, body["sources"])
		}
		if sources["rollups"] != float64(0) {
			t.Errorf("getStats(%q) rollups = %v, want 0 (hourly reads raw end to end)", q, sources["rollups"])
		}
		series, ok := body["series"].([]any)
		if !ok {
			t.Fatalf("getStats(%q) series is not an array: %T", q, body["series"])
		}
		wantLen := 25
		if strings.Contains(q, "2d") {
			wantLen = 49
		}
		if len(series) != wantLen {
			t.Errorf("getStats(%q) series len = %d, want %d hourly points", q, len(series), wantLen)
		}
		var sf, se int
		for _, v := range series {
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("getStats(%q) series point is not an object: %v", q, v)
			}
			if _, ok := m["hour"]; !ok {
				t.Errorf("getStats(%q) series point missing hour: %v", q, m)
			}
			sf += int(m["fetches"].(float64))
			se += int(m["exposures"].(float64))
		}
		raw, err := json.Marshal(body["series"])
		if err != nil {
			t.Fatalf("marshal series: %v", err)
		}
		if !strings.Contains(string(raw), `"hour":`) {
			t.Errorf("getStats(%q) series wire missing hour key: %s", q, raw)
		}
		if float64(sf) != body["fetches"] || float64(se) != body["exposures"] {
			t.Errorf("getStats(%q) series sums = %d/%d, want totals %v/%v", q, sf, se, body["fetches"], body["exposures"])
		}
	}
}

func TestDailyHandlerKeepsDayGrain(t *testing.T) {
	app, _ := flagIgnoranceApp(t)
	code, body := callStats(t, app, "?since=7d")
	if code != 200 {
		t.Fatalf("getStats(?since=7d) status = %d, want 200", code)
	}
	if echo := body["echo"].(map[string]any); echo["granularity"] != "day" {
		t.Errorf("7d granularity = %v, want day", echo["granularity"])
	}
}

// TestShortRetentionReadsRawOnly pins that when total retention is within
// the 30-day raw window, stats skips rollups entirely: no approximation,
// rollup count zero, and daily series built from raw rows.
func TestShortRetentionReadsRawOnly(t *testing.T) {
	retention.ResetForTest(retention.Config{RawDays: 7, RollupDays: 7})
	t.Cleanup(func() { retention.ResetForTest(retention.Defaults()) })

	app, _ := flagIgnoranceApp(t)
	code, body := callStats(t, app, "?since=7d")
	if code != 200 {
		t.Fatalf("getStats(?since=7d) status = %d, want 200", code)
	}
	if body["approximate"] != false {
		t.Errorf("approximate = %v, want false (raw-only)", body["approximate"])
	}
	sources, ok := body["sources"].(map[string]any)
	if !ok {
		t.Fatalf("sources is not an object: %v", body["sources"])
	}
	if sources["rollups"] != float64(0) {
		t.Errorf("rollups = %v, want 0", sources["rollups"])
	}
	if echo := body["echo"].(map[string]any); echo["granularity"] != "day" {
		t.Errorf("7d granularity = %v, want day", echo["granularity"])
	}
	series, ok := body["series"].([]any)
	if !ok {
		t.Fatalf("series is not an array: %T", body["series"])
	}
	if len(series) != 8 { // 7d window yields 8 day points
		t.Errorf("series len = %d, want 8", len(series))
	}
}
