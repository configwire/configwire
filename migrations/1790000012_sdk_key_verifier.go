package migrations

// Server-issued SDK keys with a slow verifier (ADDITIVE only).
//
// Adds sdk_keys.verifier (Text, optional: bcrypt hash string `$2a$...`)
// and sdk_keys.keyVer (Number, optional: 2 = slow verifier row).
//
// Also relaxes sdk_keys.hash to optional: rows store hash="" and MUST
// NOT store the unsalted sha256 (a DB dump of fast hashes is
// offline-brute-forceable for low-entropy keys).
//
// Deprecated (remove in v0.2.0): rows written before this migration keep
// their `hash` and authenticate via the legacy fast path; that branch is
// retained only until v0.2.0.
//
// Down is a no-op: dropping the fields would destroy stored verifiers.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("sdk_keys")
		if err != nil {
			collection, err = app.FindCollectionByNameOrId(colSDKKeys)
			if err != nil {
				return nil // collection absent; keep up idempotent
			}
		}
		changed := false
		if collection.Fields.GetByName("verifier") == nil {
			collection.Fields.Add(&core.TextField{Name: "verifier"})
			changed = true
		}
		if collection.Fields.GetByName("keyVer") == nil {
			collection.Fields.Add(&core.NumberField{Name: "keyVer", OnlyInt: true})
			changed = true
		}
		if f := collection.Fields.GetByName("hash"); f != nil {
			if tf, ok := f.(*core.TextField); ok && tf.Required {
				tf.Required = false
				changed = true
			}
		}
		if !changed {
			return nil
		}
		return app.Save(collection)
	}, func(app core.App) error {
		return nil
	})
}
