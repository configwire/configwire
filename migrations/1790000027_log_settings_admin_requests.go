package migrations

// Adds the log_settings admin-log area (Bool trio): adminLogsEnabled,
// adminLogsSuccessLines, adminLogsHealthChecks. When the area is enabled,
// admin API requests are recorded in server_logs (opt-in audit trail,
// off by default — the admin UI polls frequently, so this is high
// volume); successLines off keeps only non-2xx lines, healthChecks on
// additionally records /healthz. Stays off by default:
//   - Up adds the fields when missing, then backfills the singleton row
//     (key=global) to enabled=false, successLines=true, healthChecks=false
//     when present. Fresh installs have no row yet; EnsureLoaded seeds it
//     from the code defaults. Up is idempotent (reruns find the fields
//     and rewrite the same values).
//   - Down removes the fields (best-effort inverse; stored values are
//     dropped with it).

import (
	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

func upLogSettingsAdminRequests(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("log_settings")
	if err != nil {
		return nil // collection absent; 1790000024 owns creation
	}
	dirty := false
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		if collection.Fields.GetByName(name) == nil {
			collection.Fields.Add(&core.BoolField{Name: name})
			dirty = true
		}
	}
	if dirty {
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
	recs[0].Set("adminLogsEnabled", false)
	recs[0].Set("adminLogsSuccessLines", true)
	recs[0].Set("adminLogsHealthChecks", false)
	return app.Save(recs[0])
}

func downLogSettingsAdminRequests(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("log_settings")
	if err != nil {
		return nil // already gone; keep down idempotent
	}
	dirty := false
	for _, name := range []string{"adminLogsEnabled", "adminLogsSuccessLines", "adminLogsHealthChecks"} {
		if collection.Fields.GetByName(name) != nil {
			collection.Fields.RemoveByName(name)
			dirty = true
		}
	}
	if !dirty {
		return nil // already reverted
	}
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		return upLogSettingsAdminRequests(app)
	}, func(app core.App) error {
		return downLogSettingsAdminRequests(app)
	})
}
