package stats

import (
	"time"

	"github.com/configwire/configwire/purge"
)

// SeriesPoint is one dense daily bucket: Day is YYYY-MM-DD UTC
// (purge.DayBucket formatted), Fetches/Exposures are exact ints.
// Versions splits Fetches per day by fetched release version
// (exposures have no version); sum(Versions)==Fetches per point.
// JSON map[int]int marshals with string keys; never nil on the wire
// (zero-fetch days render {}).
type SeriesPoint struct {
	Day       string      `json:"day"`
	Fetches   int         `json:"fetches"`
	Exposures int         `json:"exposures"`
	Versions  map[int]int `json:"versions"`
}

// dayKey formats a UTC-midnight day the same way every series map keys it.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
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
// date string. Parity with Aggregate: env must match; rows with zero ts
// or ts before eventCutoff are excluded; unknown kinds are ignored.
// userHash is never read — counts only. All flags in the env count
// together (env-wide stats only).
func BuildEventSeries(rows []EventRow, envID string, eventCutoff time.Time) map[string]*SeriesPoint {
	out := map[string]*SeriesPoint{}
	for _, r := range rows {
		if r.EnvID != envID {
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
		p, ok := out[key]
		if !ok {
			p = &SeriesPoint{Day: key}
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
// zero days, days before cutoffDay, and days at/after horizon are excluded
// (boundary day stays raw-side). Counts add verbatim, no rounding.
func BuildRollupSeries(buckets []purge.Rollup, envID string, cutoffDay, horizon time.Time) map[string]*SeriesPoint {
	out := map[string]*SeriesPoint{}
	for _, b := range buckets {
		if b.EnvID != envID {
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
	out := make([]SeriesPoint, 0, len(days))
	for _, d := range days {
		key := dayKey(purge.DayBucket(d))
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
		out = append(out, SeriesPoint{Day: key, Fetches: fetches, Exposures: exposures, Versions: versions})
	}
	return out
}
