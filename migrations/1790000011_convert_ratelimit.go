package migrations

// One-time backfill of sdk_keys.fetchRps / sdk_keys.ingestRps from the
// previous per-minute quota so existing keys keep an equivalent budget:
//
//	fetchRps = ingestRps = max(1, ceil(rateLimit/60))
//
// Rules:
//   - Only rows with rateLimit > 0 are converted.
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
// ceil(n/60) with a floor of 1.
func ceilPerMinToPerSec(perMin int) int {
	if perMin <= 0 {
		return 1
	}
	if v := (perMin + 59) / 60; v >= 1 {
		return v
	}
	return 1
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
			rps := ceilPerMinToPerSec(perMin)
			changed := false
			if r.GetInt("fetchRps") == 0 {
				r.Set("fetchRps", rps)
				changed = true
			}
			if r.GetInt("ingestRps") == 0 {
				r.Set("ingestRps", rps)
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
