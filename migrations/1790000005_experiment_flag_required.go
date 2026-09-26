package migrations

// Experiment target flag required.
//
// Makes the existing experiments.flag relation Required so live DBs reject
// untargeted experiment rows (flag "") just like fresh DBs from the init
// migration. Untargeted rows are excluded from snapshots
// (ResolveExperimentFlag) and hidden in the admin list, so allowing them
// only creates dead rows — the Add experiment dialog no longer offers a
// "(none)" option and publish validation rejects an empty flag.

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("experiments")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("flag")
		rel, ok := field.(*core.RelationField)
		if !ok || rel == nil {
			return errors.New("experiments.flag is not a relation field")
		}
		rel.Required = true
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("experiments")
		if err != nil {
			return nil
		}
		field := collection.Fields.GetByName("flag")
		rel, ok := field.(*core.RelationField)
		if !ok || rel == nil {
			return nil
		}
		rel.Required = false
		return app.Save(collection)
	})
}
