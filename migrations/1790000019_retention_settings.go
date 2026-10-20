package migrations

// Retention tunables (ADDITIVE only).
//
// Adds the retention_settings collection holding the singleton row
// (key="global") that the configwire/retention package seeds and loads
// over env defaults on serve (admin API + ticker rewire rides with Wave 3).
//
// Shape: retention_settings{key Text required, rawDays Number OnlyInt,
// rollupDays Number OnlyInt}. Singleton-ness is enforced in Go code
// (find-or-create on key="global"), backed by a UNIQUE index on key
// ("unique-ish": index-level uniqueness, no Placeholder query rule).
//
// Rules: nil-deny like the other ConfigWire collections (nil = deny
// non-superusers; "" would be public). Down drops retention_settings ONLY.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		if _, err := app.FindCollectionByNameOrId("retention_settings"); err == nil {
			return nil
		}
		settings := core.NewBaseCollection("retention_settings", "cw_retention_settings")
		settings.ListRule = nil
		settings.ViewRule = nil
		settings.CreateRule = nil
		settings.UpdateRule = nil
		settings.DeleteRule = nil
		settings.Fields.Add(
			&core.TextField{Name: "key", Required: true, Presentable: true},
			&core.NumberField{Name: "rawDays", OnlyInt: true},
			&core.NumberField{Name: "rollupDays", OnlyInt: true},
		)
		settings.AddIndex("idx_retention_settings_key", true, "key", "")
		return app.Save(settings)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("retention_settings")
		if err != nil {
			return nil
		}
		return app.Delete(collection)
	})
}
