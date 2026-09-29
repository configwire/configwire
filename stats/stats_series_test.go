package stats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/configwire/configwire/purge"
)

// seriesWindow mirrors the handler's disjoint window math for one fixed now:
// cutoff = now - days*24h, horizon = DayBucket(now-30d),
// cutoffDay = DayBucket(cutoff), eventCutoff = max(cutoff, horizon).
func seriesWindow(t *testing.T, now time.Time, days int) (cutoff, cutoffDay, horizon, eventCutoff time.Time) {
	t.Helper()
	cutoff = now.Add(-time.Duration(days) * 24 * time.Hour)
	horizon = HorizonFor(now)
	cutoffDay = purge.DayBucket(cutoff)
	eventCutoff = EventCutoffFor(cutoff, horizon)
	return cutoff, cutoffDay, horizon, eventCutoff
}

func sumSeries(pts []SeriesPoint) (fetches, exposures int) {
	for _, p := range pts {
		fetches += p.Fetches
		exposures += p.Exposures
	}
	return fetches, exposures
}

func TestSeriesDaysDenseLength(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	for _, n := range []int{7, 30, 90} {
		// Last N days ending today: aligned start yields exactly N points.
		start := purge.DayBucket(now).AddDate(0, 0, -(n - 1))
		days := SeriesDays(start, now)
		if len(days) != n {
			t.Errorf("n=%d: len = %d, want %d", n, len(days), n)
			continue
		}
		for i, d := range days {
			if d.Location() != time.UTC {
				t.Errorf("n=%d day %d loc = %v, want UTC", n, i, d.Location())
			}
			if d.Hour() != 0 || d.Minute() != 0 || d.Second() != 0 || d.Nanosecond() != 0 {
				t.Errorf("n=%d day %d = %v, want UTC midnight", n, i, d)
			}
			if i > 0 && !days[i].Equal(days[i-1].AddDate(0, 0, 1)) {
				t.Errorf("n=%d day %d = %v, want +24h after %v", n, i, days[i], days[i-1])
			}
		}
		if !days[0].Equal(start) {
			t.Errorf("n=%d first = %v, want %v", n, days[0], start)
		}
		if !days[len(days)-1].Equal(purge.DayBucket(now)) {
			t.Errorf("n=%d last = %v, want %v", n, days[len(days)-1], purge.DayBucket(now))
		}
	}
}

func TestSeriesDaysHandlerCutoffCoversWindow(t *testing.T) {
	// Handler-real cutoffDay always densifies to days+1 points and covers
	// every counted bucket (no partial-first-day loss).
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	for _, n := range []int{7, 30, 90} {
		cutoff := now.Add(-time.Duration(n) * 24 * time.Hour)
		days := SeriesDays(purge.DayBucket(cutoff), now)
		if len(days) != n+1 {
			t.Errorf("n=%d: len = %d, want %d (cutoffDay..today inclusive)", n, len(days), n+1)
		}
	}
}

func TestSeriesDayBucketing(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, _, _, eventCutoff := seriesWindow(t, now, 7)
	day := purge.DayBucket(now)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Ts: day.Add(1 * time.Hour)},
		{EnvID: "e1", Kind: "fetch", Ts: day.Add(23 * time.Hour)},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: day.Add(12 * time.Hour)},
		{EnvID: "e1", Kind: "weird", Variant: "control", Ts: day.Add(13 * time.Hour)},
		{EnvID: "e1", Kind: "fetch"},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: eventCutoff.Add(-time.Hour)},
	}
	got := BuildEventSeries(rows, "e1", eventCutoff)
	if len(got) != 1 {
		t.Fatalf("event series keys = %v, want single day bucket", got)
	}
	p := got[day.Format("2006-01-02")]
	if p == nil {
		t.Fatalf("missing bucket for %s", day.Format("2006-01-02"))
	}
	if p.Day != day.Format("2006-01-02") {
		t.Errorf("Day = %q, want YYYY-MM-DD UTC", p.Day)
	}
	if p.Fetches != 2 || p.Exposures != 1 {
		t.Errorf("bucket = %+v, want {fetches:2 exposures:1} (unknown kind, zero-ts, pre-cutoff excluded)", p)
	}
}

func TestSeriesMergeSumsEqualMergeStats(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	for _, days := range []int{7, 30, 90} {
		_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, days)
		today := purge.DayBucket(now)
		oldDay := horizon.AddDate(0, 0, -5)
		midDay := today.AddDate(0, 0, -2)
		rows := []EventRow{
			{EnvID: "e1", Kind: "fetch", Ts: today.Add(10 * time.Hour)},
			{EnvID: "e1", Kind: "fetch", Ts: midDay.Add(10 * time.Hour)},
			{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: midDay.Add(11 * time.Hour)},
			{EnvID: "e1", Kind: "fetch", Ts: today.Add(9 * time.Hour)},
			{EnvID: "e2", Kind: "fetch", Ts: today.Add(9 * time.Hour)},
		}
		buckets := []purge.Rollup{
			{Day: oldDay, EnvID: "e1", Variant: "control", Fetches: 7, Exposures: 9},
			{Day: oldDay, EnvID: "e1", Variant: "treatment", Fetches: 4, Exposures: 5},
			{Day: horizon, EnvID: "e1", Variant: "control", Fetches: 11, Exposures: 13},
		}
		daysList := SeriesDays(cutoffDay, now)
		em := BuildEventSeries(rows, "e1", eventCutoff)
		rm := BuildRollupSeries(buckets, "e1", cutoffDay, horizon)
		merged := MergeSeries(em, rm, daysList)
		if len(merged) != len(daysList) {
			t.Errorf("%dd: merged len = %d, want dense %d", days, len(merged), len(daysList))
		}
		for i := 1; i < len(merged); i++ {
			if merged[i].Day <= merged[i-1].Day {
				t.Errorf("%dd: series not chronological at %d: %q after %q", days, i, merged[i].Day, merged[i-1].Day)
			}
		}
		sf, se := sumSeries(merged)
		want := MergeStats(
			Aggregate(rows, "e1", eventCutoff),
			AggregateRollups(buckets, "e1", cutoffDay, horizon),
		)
		if sf != want.Fetches || se != want.Exposures {
			t.Errorf("%dd: series sums = %d/%d, want MergeStats %d/%d", days, sf, se, want.Fetches, want.Exposures)
		}
	}
}

func TestSeriesCrashWindowDisjoint(t *testing.T) {
	// Same logical count present in BOTH events+rollups is counted once
	// after merge: pre-horizon ts never reaches the event map.
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, 90)
	oldTs := horizon.AddDate(0, 0, -5).Add(12 * time.Hour)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Ts: oldTs},
		{EnvID: "e1", Kind: "fetch", Ts: oldTs},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: oldTs},
	}
	buckets := []purge.Rollup{
		{Day: purge.DayBucket(oldTs), EnvID: "e1", Variant: "control", Fetches: 2, Exposures: 1},
	}
	em := BuildEventSeries(rows, "e1", eventCutoff)
	if len(em) != 0 {
		t.Errorf("crash-window event map = %v, want empty (ts < horizon excluded)", em)
	}
	rm := BuildRollupSeries(buckets, "e1", cutoffDay, horizon)
	merged := MergeSeries(em, rm, SeriesDays(cutoffDay, now))
	sf, se := sumSeries(merged)
	if sf != 2 || se != 1 {
		t.Errorf("crash-window merged sums = %d/%d, want 2/1 counted once", sf, se)
	}
	want := MergeStats(
		Aggregate(rows, "e1", eventCutoff),
		AggregateRollups(buckets, "e1", cutoffDay, horizon),
	)
	if sf != want.Fetches || se != want.Exposures {
		t.Errorf("crash-window series sums = %d/%d, want MergeStats %d/%d", sf, se, want.Fetches, want.Exposures)
	}
}

func TestSeriesShortWindowRollupsZero(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, 7)
	rows := []EventRow{{EnvID: "e1", Kind: "fetch", Ts: now}}
	buckets := []purge.Rollup{
		{Day: horizon.AddDate(0, 0, -1), EnvID: "e1", Variant: "control", Fetches: 7, Exposures: 9},
	}
	rm := BuildRollupSeries(buckets, "e1", cutoffDay, horizon)
	if len(rm) != 0 {
		t.Errorf("7d rollup map = %v, want empty (cutoffDay > history)", rm)
	}
	merged := MergeSeries(BuildEventSeries(rows, "e1", eventCutoff), rm, SeriesDays(cutoffDay, now))
	sf, se := sumSeries(merged)
	if sf != 1 || se != 0 {
		t.Errorf("7d merged sums = %d/%d, want 1/0", sf, se)
	}
}

func TestSeriesEnvWideSums(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, 90)
	today := purge.DayBucket(now)
	oldDay := horizon.AddDate(0, 0, -5)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Ts: today.Add(10 * time.Hour)},
		{EnvID: "e1", Kind: "fetch", Ts: today.Add(11 * time.Hour)},
		{EnvID: "e1", Kind: "exposure", Variant: "", Ts: today.Add(12 * time.Hour)},
	}
	buckets := []purge.Rollup{
		{Day: oldDay, EnvID: "e1", Variant: "control", Fetches: 1, Exposures: 1},
		{Day: oldDay, EnvID: "e1", Variant: "", Fetches: 2, Exposures: 3},
	}
	daysList := SeriesDays(cutoffDay, now)
	merged := MergeSeries(
		BuildEventSeries(rows, "e1", eventCutoff),
		BuildRollupSeries(buckets, "e1", cutoffDay, horizon),
		daysList,
	)
	sf, se := sumSeries(merged)
	want := MergeStats(
		Aggregate(rows, "e1", eventCutoff),
		AggregateRollups(buckets, "e1", cutoffDay, horizon),
	)
	if sf != want.Fetches || se != want.Exposures {
		t.Errorf("env-wide sums = %d/%d, want MergeStats %d/%d", sf, se, want.Fetches, want.Exposures)
	}
	if sf != 5 || se != 5 {
		t.Errorf("env-wide sums = %d/%d, want 5/5", sf, se)
	}
}

func TestSeriesEnvIsolation(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, 90)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Ts: now},
		{EnvID: "e2", Kind: "fetch", Ts: now},
	}
	buckets := []purge.Rollup{
		{Day: horizon.AddDate(0, 0, -5), EnvID: "e1", Variant: "control", Fetches: 3, Exposures: 4},
		{Day: horizon.AddDate(0, 0, -5), EnvID: "e2", Variant: "control", Fetches: 30, Exposures: 40},
	}
	merged := MergeSeries(
		BuildEventSeries(rows, "e1", eventCutoff),
		BuildRollupSeries(buckets, "e1", cutoffDay, horizon),
		SeriesDays(cutoffDay, now),
	)
	sf, se := sumSeries(merged)
	if sf != 4 || se != 4 {
		t.Errorf("env-isolated sums = %d/%d, want 4/4", sf, se)
	}
}

func sumVersions(p SeriesPoint) int {
	s := 0
	for _, n := range p.Versions {
		s += n
	}
	return s
}

func TestSeriesVersionSplitBucketing(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, _, _, eventCutoff := seriesWindow(t, now, 7)
	day := purge.DayBucket(now)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Version: 1, Ts: day.Add(1 * time.Hour)},
		{EnvID: "e1", Kind: "fetch", Version: 2, Ts: day.Add(2 * time.Hour)},
		{EnvID: "e1", Kind: "fetch", Version: 2, Ts: day.Add(3 * time.Hour)},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Version: 9, Ts: day.Add(4 * time.Hour)},
	}
	got := BuildEventSeries(rows, "e1", eventCutoff)
	p := got[day.Format("2006-01-02")]
	if p == nil {
		t.Fatalf("missing bucket for %s", day.Format("2006-01-02"))
	}
	if p.Fetches != 3 {
		t.Errorf("fetches = %d, want 3", p.Fetches)
	}
	if p.Versions[1] != 1 || p.Versions[2] != 2 {
		t.Errorf("versions = %v, want map[1:1 2:2]", p.Versions)
	}
	if _, ok := p.Versions[9]; ok {
		t.Errorf("versions = %v, exposure version must not be tracked", p.Versions)
	}
	if sumVersions(*p) != p.Fetches {
		t.Errorf("sum(versions) = %d, want fetches %d", sumVersions(*p), p.Fetches)
	}
}

func TestSeriesRollupVersionMerge(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, _ := seriesWindow(t, now, 90)
	oldDay := horizon.AddDate(0, 0, -5)
	key := oldDay.Format("2006-01-02")
	buckets := []purge.Rollup{
		{Day: oldDay, EnvID: "e1", Variant: "control", Version: 1, Fetches: 7, Exposures: 9},
		{Day: oldDay, EnvID: "e1", Variant: "treatment", Version: 2, Fetches: 4, Exposures: 5},
		{Day: oldDay, EnvID: "e1", Variant: "control", Version: 3, Fetches: 0, Exposures: 6},
	}
	got := BuildRollupSeries(buckets, "e1", cutoffDay, horizon)
	p := got[key]
	if p == nil {
		t.Fatalf("missing bucket for %s", key)
	}
	if p.Fetches != 11 || p.Exposures != 20 {
		t.Errorf("bucket = %+v, want {fetches:11 exposures:20}", p)
	}
	if p.Versions[1] != 7 || p.Versions[2] != 4 {
		t.Errorf("versions = %v, want map[1:7 2:4]", p.Versions)
	}
	if _, ok := p.Versions[3]; ok {
		t.Errorf("versions = %v, zero-fetch version must not create an entry", p.Versions)
	}
	if sumVersions(*p) != p.Fetches {
		t.Errorf("sum(versions) = %d, want fetches %d", sumVersions(*p), p.Fetches)
	}
}

func TestSeriesMergeVersionsParityPerVersion(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, 90)
	today := purge.DayBucket(now)
	midDay := today.AddDate(0, 0, -2)
	oldDay := horizon.AddDate(0, 0, -5)
	rows := []EventRow{
		{EnvID: "e1", Kind: "fetch", Version: 1, Ts: today.Add(10 * time.Hour)},
		{EnvID: "e1", Kind: "fetch", Version: 2, Ts: midDay.Add(10 * time.Hour)},
		{EnvID: "e1", Kind: "fetch", Version: 2, Ts: midDay.Add(11 * time.Hour)},
		{EnvID: "e1", Kind: "exposure", Variant: "control", Ts: midDay.Add(12 * time.Hour)},
	}
	buckets := []purge.Rollup{
		{Day: oldDay, EnvID: "e1", Variant: "control", Version: 1, Fetches: 7, Exposures: 9},
		{Day: oldDay, EnvID: "e1", Variant: "treatment", Version: 2, Fetches: 4, Exposures: 5},
	}
	daysList := SeriesDays(cutoffDay, now)
	merged := MergeSeries(
		BuildEventSeries(rows, "e1", eventCutoff),
		BuildRollupSeries(buckets, "e1", cutoffDay, horizon),
		daysList,
	)
	for _, p := range merged {
		if p.Versions == nil {
			t.Fatalf("day %s versions is nil, want non-nil (possibly empty) map", p.Day)
		}
		if sumVersions(p) != p.Fetches {
			t.Errorf("day %s sum(versions) = %d, want fetches %d", p.Day, sumVersions(p), p.Fetches)
		}
	}
	seriesTotals := map[int]int{}
	for _, p := range merged {
		for v, n := range p.Versions {
			seriesTotals[v] += n
		}
	}
	want := MergeStats(
		Aggregate(rows, "e1", eventCutoff),
		AggregateRollups(buckets, "e1", cutoffDay, horizon),
	)
	if len(seriesTotals) != len(want.PerVersion) {
		t.Fatalf("series versions = %v, want PerVersion %v", seriesTotals, want.PerVersion)
	}
	for v, n := range want.PerVersion {
		if seriesTotals[v] != n {
			t.Errorf("versions[%d] = %d, want PerVersion %d (full %v vs %v)", v, seriesTotals[v], n, seriesTotals, want.PerVersion)
		}
	}
}

func TestSeriesVersionsEmptyMapNotNull(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 4, 5, 0, time.UTC)
	_, cutoffDay, horizon, eventCutoff := seriesWindow(t, now, 7)
	rows := []EventRow{{EnvID: "e1", Kind: "fetch", Version: 1, Ts: now}}
	merged := MergeSeries(
		BuildEventSeries(rows, "e1", eventCutoff),
		BuildRollupSeries(nil, "e1", cutoffDay, horizon),
		SeriesDays(cutoffDay, now),
	)
	var zero *SeriesPoint
	for i := range merged {
		if merged[i].Fetches == 0 {
			zero = &merged[i]
			break
		}
	}
	if zero == nil {
		t.Fatal("no zero-fetch day in 7d window, want at least one")
	}
	if zero.Versions == nil {
		t.Fatal("zero-fetch day versions is nil, want non-nil empty map")
	}
	if len(zero.Versions) != 0 {
		t.Fatalf("zero-fetch day versions = %v, want empty map", zero.Versions)
	}
	raw, err := json.Marshal(*zero)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"versions":{}`) {
		t.Errorf("marshaled point = %s, want versions:{} present (never null)", raw)
	}
}
