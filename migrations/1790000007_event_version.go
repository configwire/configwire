package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func addVersionField(app core.App, collectionName string) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return nil
	}
	if collection.Fields.GetByName("version") != nil {
		return nil
	}
	collection.Fields.Add(&core.NumberField{Name: "version", OnlyInt: true})
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		if err := addVersionField(app, "events"); err != nil {
			return err
		}
		if err := addVersionField(app, "event_daily"); err != nil {
			return err
		}
		collection, err := app.FindCollectionByNameOrId("event_daily")
		if err != nil {
			return nil
		}
		collection.AddIndex("idx_event_daily_upsert_key", true, "day, env, flag, variant, version", "")
		return app.Save(collection)
	}, func(app core.App) error {
		return nil
	})
}
