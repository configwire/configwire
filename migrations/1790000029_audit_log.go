package migrations

// Durable admin audit trail: one dedicated collection (ADDITIVE only).
//
// Why a dedicated audit_log instead of extending server_logs: audit rows
// carry an actor, an action and a target, live longer than the 30-day
// operator log window, and must never be silenced by the high-volume
// log-store knobs. Rules stay nil (deny non-superusers), like every
// other ConfigWire collection, so only superusers (custom handlers and
// the admin UI through them) can read or write it.
//
//   - actor Text presentable: the acting superuser's email, or "system"
//     for boot/unauth actions.
//   - actorId Text: the acting _superusers record id ("" when unknown).
//   - action Text required presentable: the stable dotted action name
//     (session.login, release.publish, key.revoke, ...).
//   - targetType/targetId/targetLabel Text: what the action touched
//     (account, environment, sdk_key, settings, maintenance) plus a
//     human label (email, env slug, key prefix).
//   - status Number OnlyInt: result status (HTTP-like, 200/201/401).
//   - detail JSON: small non-secret metadata map (env/version/etag/note,
//     numeric knobs, counts). Never secrets, tokens, tickets, full SDK
//     keys, full allowlists, or PII beyond actor/target label.
//   - created/updated Autodate: created drives the newest-first view and
//     the retention cutoff.
//
// Indexes: idx_audit_created (created) for the viewer's newest-first
// pages and the retention cursor, idx_audit_action (action),
// idx_audit_actor (actor), idx_audit_target (targetType, targetId) for
// the admin filters.
//
// Up is idempotent: reruns find the collection/fields/indexes present and
// leave them untouched. Down drops the collection only (best-effort
// inverse; rows are dropped with it).

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

// auditLogCollectionName is the collection holding audit rows.
const auditLogCollectionName = "audit_log"

// auditLogCollectionID is the stable collection id (cw_audit_log).
const auditLogCollectionID = "cw_audit_log"

// auditLogIndexes maps index names to their columns and uniqueness.
var auditLogIndexes = []struct {
	name, columns string
	unique        bool
}{
	{"idx_audit_created", "created", false},
	{"idx_audit_action", "action", false},
	{"idx_audit_actor", "actor", false},
	{"idx_audit_target", "targetType, targetId", false},
}

func upAuditLog(app core.App) error {
	collection, err := app.FindCollectionByNameOrId(auditLogCollectionName)
	if err != nil {
		collection = core.NewBaseCollection(auditLogCollectionName, auditLogCollectionID)
		collection.ListRule = nil
		collection.ViewRule = nil
		collection.CreateRule = nil
		collection.UpdateRule = nil
		collection.DeleteRule = nil
	}
	fields := []core.Field{
		&core.TextField{Name: "actor", Presentable: true},
		&core.TextField{Name: "actorId"},
		&core.TextField{Name: "action", Required: true, Presentable: true},
		&core.TextField{Name: "targetType"},
		&core.TextField{Name: "targetId"},
		&core.TextField{Name: "targetLabel"},
		&core.NumberField{Name: "status", OnlyInt: true},
		&core.JSONField{Name: "detail"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	}
	for _, f := range fields {
		if collection.Fields.GetByName(f.GetName()) == nil {
			collection.Fields.Add(f)
		}
	}
	for _, idx := range auditLogIndexes {
		if collection.GetIndex(idx.name) == "" {
			collection.AddIndex(idx.name, idx.unique, idx.columns, "")
		}
	}
	return app.Save(collection)
}

func downAuditLog(app core.App) error {
	collection, err := app.FindCollectionByNameOrId(auditLogCollectionName)
	if err != nil {
		return nil // already gone; keep down idempotent
	}
	return app.Delete(collection)
}

func init() {
	m.Register(func(app core.App) error {
		return upAuditLog(app)
	}, func(app core.App) error {
		return downAuditLog(app)
	})
}
