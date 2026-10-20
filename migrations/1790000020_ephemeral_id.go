package migrations

// Ephemeral anonymous identity (anonID-only storage).
//
// Adds events.anonId + events.country for server-derived anonymous
// identity (configwire/identity daily-salted AnonID + ISO-2 country
// from headers). The legacy events.userHash column is KEPT nullable
// for compat but no longer written (saveStoredEvent stores "") — do
// NOT drop it here; a later migration removes it after backfill.
//
// event_daily is UNCHANGED (aggregate counts only, no identity).
//
// Down removes anonId + country ONLY (never touches userHash).
import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // fresh installs get shape from init; keep idempotent
		}
		changed := false
		if collection.Fields.GetByName("anonId") == nil {
			collection.Fields.Add(&core.TextField{Name: "anonId"})
			changed = true
		}
		if collection.Fields.GetByName("country") == nil {
			collection.Fields.Add(&core.TextField{Name: "country"})
			changed = true
		}
		if !changed {
			return nil
		}
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil
		}
		changed := false
		if collection.Fields.GetByName("anonId") != nil {
			collection.Fields.RemoveByName("anonId")
			changed = true
		}
		if collection.Fields.GetByName("country") != nil {
			collection.Fields.RemoveByName("country")
			changed = true
		}
		if !changed {
			return nil
		}
		return app.Save(collection)
	})
}
