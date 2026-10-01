package migrations

// Rate-limit tunables (ADDITIVE only).
//
// Adds the rate_settings collection holding the singleton row
// (key="global") that the configwire/limits admin API upserts and loads
// over env defaults on serve.
//
// Shape: rate_settings{key Text required, rps Number OnlyInt,
// burst Number OnlyInt, fetchRps Number OnlyInt, ingestRps Number
// OnlyInt, adminRps Number OnlyInt}. Singleton-ness is enforced in Go
// code (find-or-create on key="global"), backed by a UNIQUE index on
// key ("unique-ish": index-level uniqueness, no Placeholder query rule).
//
// Rules: nil-deny like the other ConfigWire collections (nil = deny
// non-superusers; "" would be public). Down drops rate_settings ONLY.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const colRateSettings = "cw_rate_settings"

func init() {
	m.Register(func(app core.App) error {
		settings := core.NewBaseCollection("rate_settings", colRateSettings)
		settings.ListRule = nil
		settings.ViewRule = nil
		settings.CreateRule = nil
		settings.UpdateRule = nil
		settings.DeleteRule = nil
		settings.Fields.Add(
			&core.TextField{Name: "key", Required: true, Presentable: true},
			&core.NumberField{Name: "rps", OnlyInt: true},
			&core.NumberField{Name: "burst", OnlyInt: true},
			&core.NumberField{Name: "fetchRps", OnlyInt: true},
			&core.NumberField{Name: "ingestRps", OnlyInt: true},
			&core.NumberField{Name: "adminRps", OnlyInt: true},
		)
		settings.AddIndex("idx_rate_settings_key", true, "key", "")
		return app.Save(settings)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("rate_settings")
		if err != nil {
			return nil
		}
		return app.Delete(collection)
	})
}
