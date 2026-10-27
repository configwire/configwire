package ingest

import "ganigeorgiev.com/pocketbase/pocketbase/core"

// RegisterFlagCacheHooks evicts the process-local flag-ID cache when a
// flags row changes. It binds OnRecordAfterCreateSuccess /
// OnRecordAfterUpdateSuccess / OnRecordAfterDeleteSuccess — the
// after-success variants, so a concurrent request cannot repopulate a
// stale entry between a pre-commit eviction and the commit (same
// discipline as RegisterKeyCacheHooks).
//
// Create evicts the (project,key) entry so a negative "" hit for a
// previously unknown key flips to the new flag ID immediately; update and
// delete additionally scan-evict by record id to cover renames and
// project moves.
func RegisterFlagCacheHooks(app core.App) {
	app.OnRecordAfterCreateSuccess("flags").BindFunc(func(e *core.RecordEvent) error {
		flagCacheDel(e.Record.GetString("project"), e.Record.GetString("key"))
		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess("flags").BindFunc(func(e *core.RecordEvent) error {
		flagCacheDel(e.Record.GetString("project"), e.Record.GetString("key"))
		EvictFlagByID(e.Record.Id)
		return e.Next()
	})
	app.OnRecordAfterDeleteSuccess("flags").BindFunc(func(e *core.RecordEvent) error {
		flagCacheDel(e.Record.GetString("project"), e.Record.GetString("key"))
		EvictFlagByID(e.Record.Id)
		return e.Next()
	})
}
