package migrations

// Restore analytics flag attribution on events/event_daily.
//
// Reverses 1790000008 (which dropped the flag relation as analytics-only
// with no consumers):
//   - events.flag and event_daily.flag come back as nullable
//     cascade-delete relations to cw_flags (as originally defined in
//     1787875200_init_configwire.go and 1790000000_event_daily.go).
//   - idx_events_env_flag_ts is re-added (env, flag, ts).
//   - idx_event_daily_upsert_key is redefined flag-aware as UNIQUE
//     (day, env, flag, variant, version) via same-name AddIndex replace.
//
// No fold on the way back up: rows written while flagless all share the
// empty-flag bucket ("" / null), so widening the upsert key cannot merge
// or collide them — pre-existing rows simply stay under the empty-flag
// bucket. Dropped flag values are NOT backfilled (impossible — the values
// were deleted by 1790000008, see its header); restored rows read back
// with flag unset until new writes attribute them.
//
// Rules: idempotent both ways (nil-collection returns nil, existing flag
// is skipped, AddIndex on the same name replaces, RemoveIndex on an
// absent name is a no-op). Down is a best-effort inverse mirroring
// dropEventFlagUp's field/index removal (no fold): it redefines the
// upsert key flagless as UNIQUE (day, env, variant, version) and drops
// the flag fields plus the env/flag/ts index. Databases holding
// flag-distinct daily rows may need a manual dedupe before down can
// rebuild the flagless UNIQUE key.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func restoreEventFlagUp(app core.App, collectionName string) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return nil // collection absent; keep up idempotent
	}
	if collection.Fields.GetByName("flag") == nil {
		collection.Fields.Add(&core.RelationField{
			Name:          "flag",
			CollectionId:  colFlags,
			MaxSelect:     1,
			Required:      false,
			CascadeDelete: true,
		})
	}
	if collectionName == "events" {
		collection.AddIndex("idx_events_env_flag_ts", false, "env, flag, ts", "")
	}
	if collectionName == "event_daily" {
		collection.AddIndex("idx_event_daily_upsert_key", true, "day, env, flag, variant, version", "")
	}
	return app.Save(collection)
}

func removeEventFlagDown(app core.App, collectionName string) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return nil // collection absent; keep down idempotent
	}
	if collectionName == "events" {
		collection.RemoveIndex("idx_events_env_flag_ts")
	}
	if collectionName == "event_daily" {
		collection.RemoveIndex("idx_event_daily_upsert_key")
		collection.AddIndex("idx_event_daily_upsert_key", true, "day, env, variant, version", "")
	}
	if collection.Fields.GetByName("flag") != nil {
		collection.Fields.RemoveByName("flag")
	}
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		if err := restoreEventFlagUp(app, "events"); err != nil {
			return err
		}
		return restoreEventFlagUp(app, "event_daily")
	}, func(app core.App) error {
		if err := removeEventFlagDown(app, "events"); err != nil {
			return err
		}
		return removeEventFlagDown(app, "event_daily")
	})
}
