package migrations

// Server-side operator log store (ADDITIVE only).
//
// Adds the server_logs collection holding persisted logx reason-code lines
// (level/message/src only — never key material, tokens, emails, IPs,
// buckets, or attributes) plus the log_settings collection holding the
// singleton row (key="global") that the configwire/logstore package seeds
// and loads over env defaults on serve (viewer + settings admin API rides
// with a later step).
//
// Shape: server_logs{level Number OnlyInt, message Text required,
// src Text, created/updated Autodate}. src defaults to "configwire" in
// Go code (Write fills empty src) — PocketBase TextField has no column
// default beyond ''. created/updated are declared explicitly (PB v0.40
// adds no record fields by itself), and Purge cutoffs compare on
// created.
// Shape: log_settings{key Text required, enabled Bool, maxDays Number
// OnlyInt}. Singleton-ness is enforced in Go code (find-or-create on
// key="global"), backed by a UNIQUE index on key.
//
// Rules: nil-deny like the other ConfigWire collections (nil = deny
// non-superusers; "" would be public). Down drops server_logs and
// log_settings ONLY.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func upServerLogs(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("server_logs"); err != nil {
		logs := core.NewBaseCollection("server_logs", "cw_server_logs")
		logs.ListRule = nil
		logs.ViewRule = nil
		logs.CreateRule = nil
		logs.UpdateRule = nil
		logs.DeleteRule = nil
		logs.Fields.Add(
			&core.NumberField{Name: "level", OnlyInt: true},
			&core.TextField{Name: "message", Required: true, Presentable: true},
			&core.TextField{Name: "src"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		logs.AddIndex("idx_server_logs_created", false, "created", "")
		if err := app.Save(logs); err != nil {
			return err
		}
	}
	if _, err := app.FindCollectionByNameOrId("log_settings"); err != nil {
		settings := core.NewBaseCollection("log_settings", "cw_log_settings")
		settings.ListRule = nil
		settings.ViewRule = nil
		settings.CreateRule = nil
		settings.UpdateRule = nil
		settings.DeleteRule = nil
		settings.Fields.Add(
			&core.TextField{Name: "key", Required: true, Presentable: true},
			&core.BoolField{Name: "enabled"},
			&core.NumberField{Name: "maxDays", OnlyInt: true},
		)
		settings.AddIndex("idx_log_settings_key", true, "key", "")
		if err := app.Save(settings); err != nil {
			return err
		}
	}
	return nil
}

func downServerLogs(app core.App) error {
	for _, name := range []string{"server_logs", "log_settings"} {
		collection, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			continue // already gone; keep down idempotent
		}
		if err := app.Delete(collection); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	m.Register(func(app core.App) error {
		return upServerLogs(app)
	}, func(app core.App) error {
		return downServerLogs(app)
	})
}
