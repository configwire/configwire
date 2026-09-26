package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

type cascadeTarget struct {
	collection string
	field      string
}

var cascadeTargets = []cascadeTarget{
	{"environments", "project"},
	{"groups", "project"},
	{"flags", "project"},
	{"releases", "env"},
	{"sdk_keys", "env"},
	{"events", "env"},
	{"events", "flag"},
	{"experiments", "flag"},
	{"event_daily", "env"},
	{"event_daily", "flag"},
	{"rules", "flag"},
}

func applyCascade(app core.App, enable bool) error {
	for _, t := range cascadeTargets {
		collection, err := app.FindCollectionByNameOrId(t.collection)
		if err != nil {
			continue
		}
		field := collection.Fields.GetByName(t.field)
		rel, ok := field.(*core.RelationField)
		if !ok || rel == nil {
			continue
		}
		if rel.CascadeDelete == enable {
			continue
		}
		rel.CascadeDelete = enable
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	m.Register(func(app core.App) error {
		return applyCascade(app, true)
	}, func(app core.App) error {
		return applyCascade(app, false)
	})
}
