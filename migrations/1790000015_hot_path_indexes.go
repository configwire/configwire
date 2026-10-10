package migrations

// Hot-path lookup indexes, round 2 (ADDITIVE only).
//
// The per-request SDK auth prefilter (`sdk_keys` by `prefix`, the
// highest-QPS lookup in the system), the release max-version scan
// (`releases` by `env, version`, hit on every publish/rollback/promote
// and stats read), and the publish-path scans (`rules` and
// `experiments` by `flag`) all run without an index today.
//
// Adds non-unique lookup indexes:
//   - sdk_keys(prefix) — per-request key prefilter.
//   - releases(env, version) — max-version-per-env lookup.
//   - rules(flag) — snapshot build groups rules by flag.
//   - experiments(flag) — snapshot build + flag-cascade fan-out.
//
// Deliberately NON-UNIQUE: unique indexes would refuse `migrate up` on
// databases holding legacy duplicate rows (duplicate flag keys, duplicate
// release versions from multi-process writes). Uniqueness stays enforced
// in Go write-path hooks; these indexes only remove the full-table scans.
//
// Rules: additive + idempotent both ways. Up uses AddIndex (same-name
// replaces, so re-running up is a no-op). Down uses RemoveIndex (absent
// name is a no-op) and never touches tables or rows. Missing collections
// are skipped so the migration stays green on partial DBs.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

type hotPathIndex struct {
	collection string
	name       string
	columns    string
}

var hotPathIndexes = []hotPathIndex{
	{"sdk_keys", "idx_sdk_keys_prefix", "prefix"},
	{"releases", "idx_releases_env_version", "env, version"},
	{"rules", "idx_rules_flag", "flag"},
	{"experiments", "idx_experiments_flag", "flag"},
}

func applyHotPathIndexes(app core.App, add bool) error {
	byCollection := map[string][]hotPathIndex{}
	for _, idx := range hotPathIndexes {
		byCollection[idx.collection] = append(byCollection[idx.collection], idx)
	}
	for name, idxs := range byCollection {
		collection, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			continue // collection absent; keep migration idempotent
		}
		for _, idx := range idxs {
			if add {
				collection.AddIndex(idx.name, false, idx.columns, "")
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
		return applyHotPathIndexes(app, true)
	}, func(app core.App) error {
		return applyHotPathIndexes(app, false)
	})
}
