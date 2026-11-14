package stats

import (
	"time"

	"github.com/configwire/configwire/purge"
)

// SeriesPoint is one dense bucket: Day is always YYYY-MM-DD UTC
// (purge.DayBucket formatted), Fetches/Exposures are exact ints.
// Hourly series (echo.granularity == "hour") additionally carry Hour, the
// RFC3339 UTC bucket start (e.g. "2026-11-14T13:00:00Z"); daily series
// omit it, so older clients that key only by Day still sum hours back to
// their day. Versions splits Fetches per bucket by fetched release
// version (exposures have no version); sum(Versions)==Fetches per point.
// JSON map[int]int marshals with string keys; never nil on the wire
// (zero-fetch buckets render {}).
type SeriesPoint struct {
	Day       string      `json:"day"`
	Hour      string      `json:"hour,omitempty"`
	Fetches   int         `json:"fetches"`
	Exposures int         `json:"exposures"`
	Versions  map[int]int `json:"versions"`
}

// dayKey formats a UTC-midnight day the same way every series map keys it.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// HourBucket truncates ts to its UTC hour (the hourly series grain).
func HourBucket(ts time.Time) time.Time {
	return ts.UTC().Truncate(time.Hour)
}

// hourKey formats a UTC hour as RFC3339, the hourly series map key and
// wire Hour value (fixed width, so lexicographic order is chronological).
func hourKey(t time.Time) string {
	return HourBucket(t).Format(time.RFC3339)
}

// HourlySeriesHours returns the dense UTC-hour window from
// HourBucket(cutoff) inclusive to HourBucket(now) inclusive, in
// chronological order. The first and last hours can be partial (rows are
// still filtered by ts >= cutoff and come from the request's now), so a
// 1d window yields 25 points and a 2d window 49; every counted row lands
// inside exactly one and sums equal the hourly totals.
func HourlySeriesHours(cutoff, now time.Time) []time.Time {
	if cutoff.IsZero() || now.IsZero() {
		return nil
	}
	start := HourBucket(cutoff)
	end := HourBucket(now)
	if end.Before(start) {
		return nil
	}
	out := make([]time.Time, 0, int(end.Sub(start)/time.Hour)+1)
	for h := start; !h.After(end); h = h.Add(time.Hour) {
		out = append(out, h)
	}
	return out
}

// SeriesDays returns the dense UTC-midnight window from DayBucket(cutoffDay)
// inclusive to DayBucket(now) inclusive, in chronological order.
//
// The handler wires cutoffDay = DayBucket(now - days*24h), which always
// yields len == days+1 (whole-day subtraction preserves time-of-day, so the
// floor shifts by exactly days). Callers that want exactly N points for an
// EffectiveSince length N pass an already-aligned start:
// DayBucket(now).AddDate(0, 0, -(N-1)). Either way every counted row lands
// inside: events with ts >= cutoff bucket at >= cutoffDay, rollups with
// day >= cutoffDay bucket inside, and nothing buckets after today.
func SeriesDays(cutoffDay, now time.Time) []time.Time {
	if cutoffDay.IsZero() || now.IsZero() {
		return nil
	}
	start := purge.DayBucket(cutoffDay)
	end := purge.DayBucket(now)
	if end.Before(start) {
		return nil
	}
	out := make([]time.Time, 0, int(end.Sub(start)/(24*time.Hour))+1)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

// BuildEventSeries folds rows into per-day counts keyed by DayBucket(ts)
// date string. Parity with Aggregate: env must match; optional flagFilter
// scopes to one verbatim flag (empty = env-wide); rows with zero ts
// or ts before eventCutoff are excluded; unknown kinds are ignored.
// userHash is never read — counts only.
func BuildEventSeries(rows []EventRow, envID string, eventCutoff time.Time, flagFilter ...string) map[string]*SeriesPoint {
	return buildEventSeries(rows, envID, eventCutoff, flagOnly(flagFilter), false)
}

// BuildEventSeriesHourly folds rows into per-hour counts keyed by
// HourBucket(ts) RFC3339. Same parity contract as BuildEventSeries
// (env/flag/cutoff/kind filters, counts only); every point carries both
// Day and Hour so day-keyed consumers still aggregate correctly.
func BuildEventSeriesHourly(rows []EventRow, envID string, eventCutoff time.Time, flagFilter ...string) map[string]*SeriesPoint {
	return buildEventSeries(rows, envID, eventCutoff, flagOnly(flagFilter), true)
}

func buildEventSeries(rows []EventRow, envID string, eventCutoff time.Time, flag string, hourly bool) map[string]*SeriesPoint {
	out := map[string]*SeriesPoint{}
	for _, r := range rows {
		if r.EnvID != envID {
			continue
		}
		if flag != "" && r.Flag != flag {
			continue
		}
		if r.Ts.IsZero() || r.Ts.Before(eventCutoff) {
			continue
		}
		var isFetch, isExposure bool
		switch r.Kind {
		case "fetch":
			isFetch = true
		case "exposure":
			isExposure = true
		default:
			continue
		}
		key := dayKey(purge.DayBucket(r.Ts))
		if hourly {
			key = hourKey(r.Ts)
		}
		p, ok := out[key]
		if !ok {
			p = &SeriesPoint{Day: dayKey(purge.DayBucket(r.Ts))}
			if hourly {
				p.Hour = key
			}
			out[key] = p
		}
		if isFetch {
			p.Fetches++
			if p.Versions == nil {
				p.Versions = map[int]int{}
			}
			p.Versions[r.Version]++
		}
		if isExposure {
			p.Exposures++
		}
	}
	return out
}

// BuildRollupSeries folds event_daily buckets into per-day counts keyed by
// DayBucket(day) date string. Parity with AggregateRollups: env must match;
// optional flagFilter scopes to one verbatim flag (empty = env-wide);
// zero days, days before cutoffDay, and days at/after horizon are excluded
// (boundary day stays raw-side). Counts add verbatim, no rounding.
func BuildRollupSeries(buckets []purge.Rollup, envID string, cutoffDay, horizon time.Time, flagFilter ...string) map[string]*SeriesPoint {
	out := map[string]*SeriesPoint{}
	flag := flagOnly(flagFilter)
	for _, b := range buckets {
		if b.EnvID != envID {
			continue
		}
		if flag != "" && b.Flag != flag {
			continue
		}
		if b.Day.IsZero() || b.Day.Before(cutoffDay) {
			continue
		}
		if !b.Day.Before(horizon) {
			continue
		}
		key := dayKey(purge.DayBucket(b.Day))
		p, ok := out[key]
		if !ok {
			p = &SeriesPoint{Day: key}
			out[key] = p
		}
		p.Fetches += b.Fetches
		p.Exposures += b.Exposures
		if b.Fetches > 0 {
			if p.Versions == nil {
				p.Versions = map[int]int{}
			}
			p.Versions[b.Version] += b.Fetches
		}
	}
	return out
}

// MergeSeries densifies the event+rollup day maps over days in chronological
// order, zero-filling days with no counts. Inputs are already windowed to
// disjoint sides of the horizon, so shared days plain-sum (crash-window
// rows present in BOTH sources land on one side only by construction:
// pre-horizon ts never reaches the event map, horizon-day buckets never
// reach the rollup map). Nil maps and days outside either map yield zeros.
func MergeSeries(eventMap, rollupMap map[string]*SeriesPoint, days []time.Time) []SeriesPoint {
	return mergeSeries(eventMap, rollupMap, days, false)
}

// MergeHourlySeries densifies the hourly event map over hours, zero-filling
// empty hours. There is no rollup side: hourly windows are served entirely
// from raw events, so every bucket sums back to the same totals Aggregate saw.
func MergeHourlySeries(eventMap map[string]*SeriesPoint, hours []time.Time) []SeriesPoint {
	return mergeSeries(eventMap, nil, hours, true)
}

func mergeSeries(eventMap, rollupMap map[string]*SeriesPoint, buckets []time.Time, hourly bool) []SeriesPoint {
	out := make([]SeriesPoint, 0, len(buckets))
	for _, b := range buckets {
		key := dayKey(purge.DayBucket(b))
		if hourly {
			key = hourKey(b)
		}
		var fetches, exposures int
		versions := map[int]int{}
		if p := eventMap[key]; p != nil {
			fetches += p.Fetches
			exposures += p.Exposures
			for v, n := range p.Versions {
				versions[v] += n
			}
		}
		if p := rollupMap[key]; p != nil {
			fetches += p.Fetches
			exposures += p.Exposures
			for v, n := range p.Versions {
				versions[v] += n
			}
		}
		point := SeriesPoint{Day: dayKey(purge.DayBucket(b)), Fetches: fetches, Exposures: exposures, Versions: versions}
		if hourly {
			point.Hour = key
		}
		out = append(out, point)
	}
	return out
}
