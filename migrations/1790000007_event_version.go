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
		return addVersionField(app, "event_daily")
	}, func(app core.App) error {
		return nil
	})
}
