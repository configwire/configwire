package migrations

// Daily rollup state + the indexes the day-walk and the whole-day scan
// need (ADDITIVE only).
//
// Why: completed days are rolled into event_daily by the daily retention
// job, so stats can read history from the aggregates instead of raw
// events. That needs two things this migration adds:
//
//   - rollup_state{key Text required, lastDay Date, updated Autodate}:
//     the singleton row (key="global") holding the last completed day the
//     rollup job has persisted. Unique idx_rollup_state_key on key keeps
//     the singleton find-or-create idempotent. Rules stay nil (deny
//     non-superusers), like the other ConfigWire collections.
//   - idx_event_daily_env_day on event_daily (env, day): the stats read
//     filters one env over a day range.
//   - idx_events_ts on events (ts): the rollup job scans one whole UTC
//     day across ALL envs. The existing idx_events_env_ts /
//     idx_events_env_flag_ts are env-prefixed, so they cannot serve a
//     day-only range scan; ts alone can.
//
// Up is idempotent: reruns find the collection/fields/indexes present and
// leave them untouched. Down removes the two indexes and drops the
// collection only (best-effort inverse; rows are dropped with it).

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

// rollupStateCollectionName is the singleton collection holding the
// high-water mark of the daily rollup walk.
const rollupStateCollectionName = "rollup_state"

// rollupStateIndexes maps collections to the indexes this migration adds.
var rollupStateIndexes = [][3]string{
	// {collection, index name, columns}
	{"event_daily", "idx_event_daily_env_day", "env, day"},
	{"events", "idx_events_ts", "ts"},
}

func upRollupState(app core.App) error {
	collection, err := app.FindCollectionByNameOrId(rollupStateCollectionName)
	if err != nil {
		collection = core.NewBaseCollection(rollupStateCollectionName, "cw_rollup_state")
		collection.ListRule = nil
		collection.ViewRule = nil
		collection.CreateRule = nil
		collection.UpdateRule = nil
		collection.DeleteRule = nil
		collection.Fields.Add(
			&core.TextField{Name: "key", Required: true, Presentable: true},
			&core.DateField{Name: "lastDay"},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		collection.AddIndex("idx_rollup_state_key", true, "key", "")
	}
	if collection.GetIndex("idx_rollup_state_key") == "" {
		collection.AddIndex("idx_rollup_state_key", true, "key", "")
	}
	if err := app.Save(collection); err != nil {
		return err
	}

	for _, idx := range rollupStateIndexes {
		target, err := app.FindCollectionByNameOrId(idx[0])
		if err != nil {
			continue // sibling collection absent; its own migration owns creation
		}
		if target.GetIndex(idx[1]) != "" {
			continue
		}
		target.AddIndex(idx[1], false, idx[2], "")
		if err := app.Save(target); err != nil {
			return err
		}
	}
	return nil
}

func downRollupState(app core.App) error {
	for _, idx := range rollupStateIndexes {
		target, err := app.FindCollectionByNameOrId(idx[0])
		if err != nil {
			continue // already gone
		}
		if target.GetIndex(idx[1]) == "" {
			continue
		}
		target.RemoveIndex(idx[1])
		if err := app.Save(target); err != nil {
			return err
		}
	}

	collection, err := app.FindCollectionByNameOrId(rollupStateCollectionName)
	if err != nil {
		return nil // already gone; keep down idempotent
	}
	return app.Delete(collection)
}

func init() {
	m.Register(func(app core.App) error {
		return upRollupState(app)
	}, func(app core.App) error {
		return downRollupState(app)
	})
}
