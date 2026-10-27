package migrations

// Drop orphaned identity storage: the identity_salts collection and the
// events.userHash column.
//
// Neither is read or written by any serve path anymore:
//   - identity_salts fed the removed daily-salted anonID derivation
//     (configwire/identity salt manager); bucketing is ?b=-only now, so
//     no code loads, saves, or prunes these rows.
//   - events.userHash always stored "" (saveStoredEvent never persisted
//     the client value); the ingest API still accepts a body/query
//     userHash as accepted-but-ignored compat, but nothing reaches the
//     store. events.anonId held the stable ?b= bucket (renamed to
//     events.bucket by a later migration) and
//     events.country are untouched.
//   - event_daily never carried identity and is unchanged.
//
// Dropped values are analytics-only with no consumers; their loss is
// accepted and intended. Down re-creates the collection (nullable fields
// as originally defined) and re-adds the events.userHash column as a
// best-effort inverse so `migrate down` works; it cannot restore the
// dropped rows/values.

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

func dropIdentityStorageUp(app core.App) error {
	if collection, err := app.FindCollectionByNameOrId("identity_salts"); err == nil {
		if err := app.Delete(collection); err != nil {
			return err
		}
	}
	collection, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return nil // collection absent; keep up idempotent
	}
	if collection.Fields.GetByName("userHash") == nil {
		return nil
	}
	collection.Fields.RemoveByName("userHash")
	return app.Save(collection)
}

func addIdentityStorageDown(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return nil
	}
	if collection.Fields.GetByName("userHash") == nil {
		collection.Fields.Add(&core.TextField{Name: "userHash"})
	}
	return app.Save(collection)
}

func addIdentitySaltsDown(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("identity_salts"); err == nil {
		return nil
	}
	salts := core.NewBaseCollection("identity_salts", colIdentitySalts)
	salts.ListRule = nil
	salts.ViewRule = nil
	salts.CreateRule = nil
	salts.UpdateRule = nil
	salts.DeleteRule = nil
	salts.Fields.Add(
		&core.TextField{Name: "envID", Required: true, Presentable: true},
		&core.TextField{Name: "day", Required: true, Presentable: true},
		&core.TextField{Name: "salt", Required: true},
	)
	salts.AddIndex("idx_identity_salts_env_day", true, "envID, day", "")
	return app.Save(salts)
}

func init() {
	m.Register(func(app core.App) error {
		return dropIdentityStorageUp(app)
	}, func(app core.App) error {
		if err := addIdentitySaltsDown(app); err != nil {
			return err
		}
		return addIdentityStorageDown(app)
	})
}
