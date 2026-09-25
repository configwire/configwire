package migrations

// Flag description metadata.
//
// Adds an optional flags.description text field so the admin flag dialog
// can store a human-readable note per flag (fresh DBs get it from the
// init migration directly). Description is metadata only — it is never
// included in release snapshots or SDK evaluation.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("flags")
		if err != nil {
			return err
		}
		if collection.Fields.GetByName("description") != nil {
			return nil // already applied; keep up idempotent
		}
		collection.Fields.Add(
			&core.TextField{Name: "description", Max: 1024},
		)
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("flags")
		if err != nil {
			return nil
		}
		field := collection.Fields.GetByName("description")
		if field == nil {
			return nil
		}
		collection.Fields.RemoveByName("description")
		return app.Save(collection)
	})
}
