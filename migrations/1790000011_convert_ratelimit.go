package migrations

// One-time backfill of sdk_keys.fetchRps / sdk_keys.ingestRps from the
// previous per-minute quota so existing keys are never stricter than
// fresh installs:
//
//	fetchRps = max(100, ceil(rateLimit/60))
//	ingestRps = max(50, ceil(rateLimit/60))
//
// The floor at the global defaults (fetch 100/s, ingest 50/s) preserves
// bursty clients: a bare ceil(rateLimit/60) would turn the old default
// rateLimit=60 (60 burst/min) into 1/1 per-sec, 100x stricter than new
// keys. Only rows with rateLimit > 0 are converted.
// Rules:
//   - Rows where fetchRps/ingestRps are already set (non-zero) keep their
//     per-key overrides; only missing (zero) fields are filled.
//   - Idempotent: re-running changes nothing (converted rows no longer
//     match the missing-field predicate).
//   - Down is a no-op: dropping the backfilled values would destroy data.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// ceilPerMinToPerSec converts a per-minute quota to per-second,
// ceil(n/60) floored at the global defaults so upgraded keys match
// fresh installs (fetch 100/s, ingest 50/s).
func ceilPerMinToPerSec(perMin, floor int) int {
	if perMin <= 0 {
		return floor
	}
	if v := (perMin + 59) / 60; v > floor {
		return v
	}
	return floor
}

func init() {
	m.Register(func(app core.App) error {
		if _, err := app.FindCollectionByNameOrId("sdk_keys"); err != nil {
			return nil // collection absent; keep up idempotent
		}
		recs, err := app.FindAllRecords("sdk_keys")
		if err != nil {
			return err
		}
		for _, r := range recs {
			perMin := r.GetInt("rateLimit")
			if perMin <= 0 {
				continue
			}
			fetchRps := ceilPerMinToPerSec(perMin, 100)
			ingestRps := ceilPerMinToPerSec(perMin, 50)
			changed := false
			if r.GetInt("fetchRps") == 0 {
				r.Set("fetchRps", fetchRps)
				changed = true
			}
			if r.GetInt("ingestRps") == 0 {
				r.Set("ingestRps", ingestRps)
				changed = true
			}
			if !changed {
				continue
			}
			if err := app.Save(r); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		return nil
	})
}
