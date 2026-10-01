package migrations

// Per-API-key request/sec overrides (ADDITIVE only).
//
// Adds sdk_keys.fetchRps and sdk_keys.ingestRps (Number, OnlyInt,
// optional): when set to 1..10000 they override the global FetchRps /
// IngestRps tunables for that key (see limits.EffectiveFetchRps and
// limits.EffectiveIngestRps); zero/missing falls back to global, so the
// Admin UI shows 0 = global. No extra validation here: PocketBase checks
// the number type on write, and the readers clamp out-of-range values to
// global instead of rejecting requests.
//
// Down is a no-op: dropping the fields would destroy stored overrides.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("sdk_keys")
		if err != nil {
			collection, err = app.FindCollectionByNameOrId(colSDKKeys)
			if err != nil {
				return nil // collection absent; keep up idempotent
			}
		}
		changed := false
		if collection.Fields.GetByName("fetchRps") == nil {
			collection.Fields.Add(&core.NumberField{Name: "fetchRps", OnlyInt: true})
			changed = true
		}
		if collection.Fields.GetByName("ingestRps") == nil {
			collection.Fields.Add(&core.NumberField{Name: "ingestRps", OnlyInt: true})
			changed = true
		}
		if !changed {
			return nil
		}
		return app.Save(collection)
	}, func(app core.App) error {
		return nil
	})
}
