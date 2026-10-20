package migrations

// Country-code header order (ADDITIVE only).
//
// Adds rate_settings.countryHeaders (Text, optional): a JSON array string
// holding the ordered country-code header list (e.g.
// ["CF-IPCountry","X-Country"]) that the configwire/limits admin API
// upserts and loads over env defaults on serve. Missing/empty keeps the
// previous behavior, so rows written before this migration resolve
// exactly as before.
//
// Rules stay nil-deny like the base rate_settings collection (nil = deny
// non-superusers; "" would be public): this migration touches fields
// only, never rules.
//
// Down is a no-op: dropping the field would destroy stored orders.

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
		if collection.Fields.GetByName("countryHeaders") != nil {
			return nil
		}
		collection.Fields.Add(&core.TextField{Name: "countryHeaders"})
		return app.Save(collection)
	}, func(app core.App) error {
		return nil
	})
}
