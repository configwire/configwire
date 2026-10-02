package migrations

// Admin IP allowlist storage (ADDITIVE only).
//
// Adds rate_settings.adminAllowedIPs (Text, optional): a JSON array string
// holding the admin IP allowlist (e.g. ["10.0.0.1","192.168.1.0/24"])
// that the configwire/limits admin API upserts and loads over env
// defaults on serve. Empty/missing means allow all (no restriction), so
// rows written before this migration resolve exactly as before.
//
// Rules stay nil-deny like the base rate_settings collection (nil = deny
// non-superusers; "" would be public): this migration touches fields
// only, never rules, and never drops/recreates the collection. The
// adminRps column is left untouched for back-compat. Singleton-ness is
// enforced in Go code (find-or-create on key="global").
//
// Down removes only the adminAllowedIPs field.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("rate_settings")
		if err != nil {
			collection, err = app.FindCollectionByNameOrId(colRateSettings)
			if err != nil {
				return nil // collection absent; keep up idempotent
			}
		}
		if collection.Fields.GetByName("adminAllowedIPs") != nil {
			return nil
		}
		collection.Fields.Add(&core.TextField{Name: "adminAllowedIPs"})
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("rate_settings")
		if err != nil {
			collection, err = app.FindCollectionByNameOrId(colRateSettings)
			if err != nil {
				return nil
			}
		}
		if collection.Fields.GetByName("adminAllowedIPs") == nil {
			return nil
		}
		collection.Fields.RemoveByName("adminAllowedIPs")
		return app.Save(collection)
	})
}
