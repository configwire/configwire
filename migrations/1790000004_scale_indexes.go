package migrations

// Scale indexes for retention/stats hot paths (ADDITIVE only).
//
// Adds lookup indexes so the purge rollup, stats queries, and env/flag
// resolution stop doing full-table scans (prior finding: zero Index hits
// in the repo — every read was FindAllRecords):
//   - events(env, ts) and events(env, flag, ts) — purge cutoff scan +
//     stats windows filter on env/flag/ts.
//   - event_daily(day, env, flag, variant) UNIQUE — the upsert key currently
//     enforced in Go (purge.go); the DB now enforces it too (unset flags
//     store as '' NOT NULL, so the key is fully covered).
//   - event_daily(day) — 90d rollup-retention sweep.
//   - environments(slug, project) and flags(key, project) — per-request
//     env/flag lookups.
//
// Rules: additive + idempotent both ways. Up uses AddIndex (same-name
// replaces, so re-running up is a no-op). Down uses RemoveIndex (absent
// name is a no-op) and never touches tables or rows. Missing collections
// are skipped so down stays green on DBs where event_daily was dropped by
// the 1790000000 down migration.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

type scaleIndex struct {
	collection string
	name       string
	unique     bool
	columns    string
}

var scaleIndexes = []scaleIndex{
	{"events", "idx_events_env_ts", false, "env, ts"},
	{"events", "idx_events_env_flag_ts", false, "env, flag, ts"},
	{"event_daily", "idx_event_daily_upsert_key", true, "day, env, flag, variant"},
	{"event_daily", "idx_event_daily_day", false, "day"},
	{"environments", "idx_environments_slug_project", false, "slug, project"},
	{"flags", "idx_flags_key_project", false, "key, project"},
}

func applyScaleIndexes(app core.App, add bool) error {
	byCollection := map[string][]scaleIndex{}
	for _, idx := range scaleIndexes {
		byCollection[idx.collection] = append(byCollection[idx.collection], idx)
	}
	for name, idxs := range byCollection {
		collection, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			continue // collection absent; keep migration idempotent
		}
		for _, idx := range idxs {
			if add {
				collection.AddIndex(idx.name, idx.unique, idx.columns, "")
			} else {
				collection.RemoveIndex(idx.name)
			}
		}
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	m.Register(func(app core.App) error {
		return applyScaleIndexes(app, true)
	}, func(app core.App) error {
		return applyScaleIndexes(app, false)
	})
}
