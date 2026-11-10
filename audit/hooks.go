package audit

// Data-API hooks: SDK key revoke/unrevoke/update/delete issued through
// the PocketBase data API (the admin UI's key screens) produce audit rows
// with the acting superuser as actor. Server-side saves bypass these
// request-level hooks by design (the mint handler covers key.mint).
//
// Sequence: read the unauthenticated decision inputs BEFORE e.Next()
// (the DB row still holds the pre-update values; PocketBase has already
// applied the submitted data onto e.Record), then write only after
// e.Next() returns nil, so a rejected write never leaves an audit row.
// No auth context (e.g. a cascaded delete) skips the audit entirely.

import (
	"fmt"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// recordAuditSpec maps one admin-edited collection to its audit target and a
// human label. The data API is the only write path for these records (the
// admin UI uses it), so request hooks cover every UI edit with an actor.
type recordAuditSpec struct {
	collection string
	targetType string
	label      func(*core.Record) string
}

var recordAuditSpecs = []recordAuditSpec{
	{"flags", "flag", func(r *core.Record) string { return r.GetString("key") }},
	{"rules", "rule", func(r *core.Record) string {
		if flag := r.GetString("flag"); flag != "" {
			return flag
		}
		return r.Id
	}},
	{"experiments", "experiment", func(r *core.Record) string { return r.GetString("name") }},
	{"projects", "project", func(r *core.Record) string { return r.GetString("name") }},
	{"environments", "environment", func(r *core.Record) string { return r.GetString("slug") }},
}

// recordChangeFields lists the changed columns by name, so a config edit is
// attributable without copying flag values, variant JSON or rollout data into
// the trail. Autodate bookkeeping and the id are not edits.
func recordChangeFields(old, newRec *core.Record) []string {
	var changed []string
	for _, f := range newRec.Collection().Fields {
		name := f.GetName()
		switch name {
		case "id", "created", "updated":
			continue
		}
		if fmt.Sprint(old.Get(name)) != fmt.Sprint(newRec.Get(name)) {
			changed = append(changed, name)
		}
	}
	return changed
}

// registerRecordAudit binds create/update/delete audit hooks for one
// collection. Same sequence as the sdk_keys hooks: read the pre-update image
// before e.Next(), write only after a successful save, and skip requests with
// no auth context (cascaded deletes).
func registerRecordAudit(app core.App, spec recordAuditSpec) {
	app.OnRecordCreateRequest(spec.collection).BindFunc(func(e *core.RecordRequestEvent) error {
		info, _ := e.RequestInfo()
		if info == nil || info.Auth == nil {
			return e.Next()
		}
		if err := e.Next(); err != nil {
			return err
		}
		email, id := ActorFromRequestInfo(info)
		RecordApp(e.App, Entry{
			Actor:       email,
			ActorID:     id,
			Action:      spec.targetType + ".create",
			TargetType:  spec.targetType,
			TargetID:    e.Record.Id,
			TargetLabel: spec.label(e.Record),
			Status:      200,
		})
		return nil
	})

	app.OnRecordUpdateRequest(spec.collection).BindFunc(func(e *core.RecordRequestEvent) error {
		info, _ := e.RequestInfo()
		if info == nil || info.Auth == nil {
			return e.Next()
		}
		old, err := e.App.FindRecordById(spec.collection, e.Record.Id)
		if err != nil {
			return e.Next()
		}
		changed := recordChangeFields(old, e.Record)
		if err := e.Next(); err != nil {
			return err
		}
		if len(changed) == 0 {
			return nil // no auditable field changed
		}
		email, id := ActorFromRequestInfo(info)
		RecordApp(e.App, Entry{
			Actor:       email,
			ActorID:     id,
			Action:      spec.targetType + ".update",
			TargetType:  spec.targetType,
			TargetID:    e.Record.Id,
			TargetLabel: spec.label(e.Record),
			Status:      200,
			Detail:      map[string]any{"fields": changed},
		})
		return nil
	})

	app.OnRecordDeleteRequest(spec.collection).BindFunc(func(e *core.RecordRequestEvent) error {
		info, _ := e.RequestInfo()
		if info == nil || info.Auth == nil {
			return e.Next()
		}
		targetID := e.Record.Id
		targetLabel := spec.label(e.Record)
		if err := e.Next(); err != nil {
			return err
		}
		email, id := ActorFromRequestInfo(info)
		RecordApp(e.App, Entry{
			Actor:       email,
			ActorID:     id,
			Action:      spec.targetType + ".delete",
			TargetType:  spec.targetType,
			TargetID:    targetID,
			TargetLabel: targetLabel,
			Status:      200,
		})
		return nil
	})
}

// auditedKeyFields are the sdk_keys columns whose change makes an edit
// sensitive. The rate limits keep their values in Detail; the rest are named
// only (hash/verifier are credentials-adjacent and never echoed).
var auditedKeyFields = []string{"env", "prefix", "hash", "rateLimit", "verifier", "keyVer", "fetchRps", "ingestRps"}

// keyChangeDetail reports the sensitive sdk_keys columns changed by the edit.
// Rate-limit values are included because they are the operator intent; every
// other column is named only, so hash/verifier values never reach the trail.
func keyChangeDetail(old, newRec *core.Record, changed []string) map[string]any {
	detail := map[string]any{}
	if len(changed) > 0 {
		detail["fields"] = changed
	}
	if old.GetInt("fetchRps") != newRec.GetInt("fetchRps") {
		detail["fetchRps"] = newRec.GetInt("fetchRps")
	}
	if old.GetInt("ingestRps") != newRec.GetInt("ingestRps") {
		detail["ingestRps"] = newRec.GetInt("ingestRps")
	}
	return detail
}

// RegisterHooks binds the audit_log write guard, the sdk_keys data-API hooks
// and the flag/rule/experiment/project/environment record hooks. Called once
// from main's registerConfigwireHooks.
func RegisterHooks(app core.App) {
	// The trail is append-only. The data API is superuser-reachable (nil
	// rules), so without this guard a superuser token could rewrite or erase
	// rows and the tampering would itself be unaudited. Server-side
	// audit.Write/Purge use app.Save/app.Delete, which never fire request
	// hooks.
	denyWrite := func(e *core.RecordRequestEvent) error {
		return e.ForbiddenError("audit_log is append-only.", nil)
	}
	app.OnRecordCreateRequest(collectionName).BindFunc(denyWrite)
	app.OnRecordUpdateRequest(collectionName).BindFunc(denyWrite)
	app.OnRecordDeleteRequest(collectionName).BindFunc(denyWrite)

	app.OnRecordUpdateRequest("sdk_keys").BindFunc(func(e *core.RecordRequestEvent) error {
		info, _ := e.RequestInfo()
		if info == nil || info.Auth == nil {
			return e.Next()
		}
		// e.Record already carries the submitted values; the DB row is
		// still the pre-update image until e.Next() saves it.
		old, err := e.App.FindRecordById("sdk_keys", e.Record.Id)
		if err != nil {
			return e.Next()
		}
		oldRevoked := old.GetBool("revoked")
		var changed []string
		for _, f := range auditedKeyFields {
			if fmt.Sprint(old.Get(f)) != fmt.Sprint(e.Record.Get(f)) {
				changed = append(changed, f)
			}
		}

		if err := e.Next(); err != nil {
			return err
		}

		newRevoked := e.Record.GetBool("revoked")
		action := ""
		var detail map[string]any
		switch {
		case oldRevoked != newRevoked:
			if newRevoked {
				action = "key.revoke"
			} else {
				action = "key.unrevoke"
			}
			detail = keyChangeDetail(old, e.Record, changed)
			detail["revoked"] = newRevoked
		case len(changed) > 0:
			action = "key.update"
			detail = keyChangeDetail(old, e.Record, changed)
		default:
			return nil // no auditable field changed
		}
		email, id := ActorFromRequestInfo(info)
		RecordApp(e.App, Entry{
			Actor:       email,
			ActorID:     id,
			Action:      action,
			TargetType:  "sdk_key",
			TargetID:    e.Record.Id,
			TargetLabel: e.Record.GetString("prefix"),
			Status:      200,
			Detail:      detail,
		})
		return nil
	})

	for _, spec := range recordAuditSpecs {
		registerRecordAudit(app, spec)
	}

	app.OnRecordDeleteRequest("sdk_keys").BindFunc(func(e *core.RecordRequestEvent) error {
		info, _ := e.RequestInfo()
		if info == nil || info.Auth == nil {
			return e.Next()
		}
		// Capture the target before the delete clears the record.
		targetID := e.Record.Id
		targetLabel := e.Record.GetString("prefix")
		if err := e.Next(); err != nil {
			return err
		}
		email, id := ActorFromRequestInfo(info)
		RecordApp(e.App, Entry{
			Actor:       email,
			ActorID:     id,
			Action:      "key.delete",
			TargetType:  "sdk_key",
			TargetID:    targetID,
			TargetLabel: targetLabel,
			Status:      200,
		})
		return nil
	})
}
