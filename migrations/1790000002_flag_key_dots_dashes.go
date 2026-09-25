package migrations

// Flag key shape: allow hyphens and dots.
//
// Updates the existing flags.key Pattern so live DBs accept kebab-case
// and dotted keys (e.g. `latest-version`, `my.flag-name`) just like
// fresh DBs from the init migration. Code hooks in main.go and
// releases/snapshot.go enforce the same shape.

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const flagKeyPatternNew = `^[A-Za-z_][A-Za-z0-9_.-]*$`
const flagKeyPatternOld = `^[A-Za-z_][A-Za-z0-9_]*$`

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("flags")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("key")
		text, ok := field.(*core.TextField)
		if !ok || text == nil {
			return errors.New("flags.key is not a text field")
		}
		text.Pattern = flagKeyPatternNew
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("flags")
		if err != nil {
			return nil
		}
		field := collection.Fields.GetByName("key")
		text, ok := field.(*core.TextField)
		if !ok || text == nil {
			return nil
		}
		text.Pattern = flagKeyPatternOld
		return app.Save(collection)
	})
}
