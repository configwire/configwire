package migrations

// Rename events.anonId to events.bucket, preserving values.
//
// The column always held the stable ?b= bucket (never a derived anonID
// anymore), so the name finally matches the content. No behavior change:
//   - Up adds the events.bucket Text field, copies every non-empty
//     events.anonId value onto it (rows already empty are skipped, so
//     large tables rewrite only dirty rows), then drops events.anonId.
//     Copy-then-drop is idempotent: a rerun finds no anonId field and
//     returns nil, and re-copied values are identical.
//   - event_daily never carried identity and is unchanged; no index
//     references anonId, so no index is rebuilt.
//   - Down is a best-effort inverse (re-add anonId, copy bucket back,
//     drop bucket); it cannot restore rows deleted meanwhile.
//
// PAGING: both directions backfill via an id-keyset cursor
// (FindRecordsByFilter filter `id > {:cursor}`, sort "id", LIMIT 500):
// each page collects dirty rows (non-empty source only, empties skipped)
// and commits them in one short RunInTransaction, so heap stays flat and
// the write lock is never held for the whole table. id never changes, so
// in-place copies cannot shift the cursor; a rerun converges.

import (
	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

// renamePageSize bounds one cursor page: one page = one short transaction.
const renamePageSize = 500

// copyFieldPaged copies every non-empty src value onto dst over the whole
// events table via id-keyset paging, one short transaction per page.
// Empty source rows are skipped (never rewritten). An empty table commits
// nothing and returns nil.
func copyFieldPaged(app core.App, src, dst string) error {
	cursor := ""
	hasCursor := false
	for {
		filter := ""
		params := dbx.Params{}
		if hasCursor {
			filter = "id > {:cursor}"
			params["cursor"] = cursor
		}
		page, err := app.FindRecordsByFilter("events", filter, "id", renamePageSize, 0, params)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		var dirty []*core.Record
		var vals []string
		for _, r := range page {
			if v := r.GetString(src); v != "" {
				dirty = append(dirty, r)
				vals = append(vals, v)
			}
		}
		last := page[len(page)-1]
		cursor = last.Id
		hasCursor = true
		if len(dirty) > 0 {
			if err := app.RunInTransaction(func(txApp core.App) error {
				for i, r := range dirty {
					r.Set(dst, vals[i])
					if err := txApp.Save(r); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if len(page) < renamePageSize {
			break
		}
	}
	return nil
}

func renameAnonIdToBucketUp(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return nil // collection absent; keep up idempotent
	}
	if collection.Fields.GetByName("bucket") == nil {
		collection.Fields.Add(&core.TextField{Name: "bucket"})
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	if collection.Fields.GetByName("anonId") == nil {
		return nil // already renamed
	}
	if err := copyFieldPaged(app, "anonId", "bucket"); err != nil {
		return err
	}
	collection.Fields.RemoveByName("anonId")
	return app.Save(collection)
}

func renameBucketToAnonIdDown(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return nil
	}
	if collection.Fields.GetByName("anonId") == nil {
		collection.Fields.Add(&core.TextField{Name: "anonId"})
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	if collection.Fields.GetByName("bucket") == nil {
		return nil // already reverted
	}
	if err := copyFieldPaged(app, "bucket", "anonId"); err != nil {
		return err
	}
	collection.Fields.RemoveByName("bucket")
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		return renameAnonIdToBucketUp(app)
	}, func(app core.App) error {
		return renameBucketToAnonIdDown(app)
	})
}
