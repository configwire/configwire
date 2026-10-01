package ingest

import "github.com/pocketbase/pocketbase/core"

// RegisterKeyCacheHooks evicts the process-local SDK-key fast cache when an
// sdk_keys row changes. It binds OnRecordAfterUpdateSuccess /
// OnRecordAfterDeleteSuccess (PocketBase v0.40.4 core/app.go:1082,1112) —
// the after-success variants, so a concurrent request cannot repopulate a
// stale entry between a pre-commit eviction and the commit.
func RegisterKeyCacheHooks(app core.App) {
	app.OnRecordAfterUpdateSuccess("sdk_keys").BindFunc(func(e *core.RecordEvent) error {
		EvictKeyByID(e.Record.Id)
		return e.Next()
	})
	app.OnRecordAfterDeleteSuccess("sdk_keys").BindFunc(func(e *core.RecordEvent) error {
		EvictKeyByID(e.Record.Id)
		return e.Next()
	})
}
