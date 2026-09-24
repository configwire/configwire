package migrations

// Flag rules cascade delete.
//
// Updates the existing cw_rules.flag relation field to CascadeDelete=true
// so deleting a flag automatically deletes its child rules on live DBs
// (fresh DBs get the option from the init migration directly).

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("cw_rules")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("flag")
		rel, ok := field.(*core.RelationField)
		if !ok || rel == nil {
			return errors.New("cw_rules.flag is not a relation field")
		}
		rel.CascadeDelete = true
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("cw_rules")
		if err != nil {
			return nil
		}
		field := collection.Fields.GetByName("flag")
		rel, ok := field.(*core.RelationField)
		if !ok || rel == nil {
			return nil
		}
		rel.CascadeDelete = false
		return app.Save(collection)
	})
}
