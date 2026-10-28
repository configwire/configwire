package migrations

// Adds log_settings.successLogs (Bool): when false, success lines
// (fetch served/cached, ingest accepted) skip the server_logs store
// while rejections keep flowing. Stays on by default:
//   - Up adds the field when missing, then backfills the singleton row
//     (key=global) to true when present. The backfill is unconditional:
//     false was unrepresentable before this field existed, so any stored
//     row means "on". Fresh installs have no row yet; EnsureLoaded seeds
//     it from the code default (true). Up is idempotent (reruns find the
//     field and rewrite the same true).
//   - Down removes the field (best-effort inverse; stored values are
//     dropped with it).

import (
	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

func upLogSettingsSuccess(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("log_settings")
	if err != nil {
		return nil // collection absent; 1790000024 owns creation
	}
	if collection.Fields.GetByName("successLogs") == nil {
		collection.Fields.Add(&core.BoolField{Name: "successLogs"})
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	recs, err := app.FindRecordsByFilter("log_settings", "key = {:key}", "", 1, 0, dbx.Params{"key": "global"})
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil // no singleton row yet; EnsureLoaded seeds it
	}
	recs[0].Set("successLogs", true)
	return app.Save(recs[0])
}

func downLogSettingsSuccess(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("log_settings")
	if err != nil {
		return nil // already gone; keep down idempotent
	}
	if collection.Fields.GetByName("successLogs") == nil {
		return nil // already reverted
	}
	collection.Fields.RemoveByName("successLogs")
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		return upLogSettingsSuccess(app)
	}, func(app core.App) error {
		return downLogSettingsSuccess(app)
	})
}
