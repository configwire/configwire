package migrations

// Server-side operator log store: dedicated filter columns (ADDITIVE only).
//
// Replaces the message-token LIKE filtering with indexed equality columns
// on server_logs: project/env/key_prefix/status/reason/limiter sit beside
// the level/message/src row the writers already emit (the message line
// format is unchanged — new columns are populated alongside it, and the
// viewer keeps parsing message tokens for display only).
//
// Why: LIKE on the message ("status=20" prefix-matches "status=200") cannot
// express exact matches, and it scans instead of seeking. Equality on one
// non-unique index per column is exact (status 20 never matches 200) and
// indexed.
//
//   - Up adds the six nullable fields when missing (project Relation
//     single-select to projects, no cascade — project deletion must
//     never wipe log history; env Relation single-select to
//     environments, no cascade — env deletion must never wipe log
//     history; key_prefix Text, status Number OnlyInt, reason Text,
//     limiter Text), adds one non-unique index per column when missing
//     (PocketBase does NOT auto-index relation fields —
//     core/field_relation.go ColumnType is plain TEXT and
//     collection_record_table_sync only builds declared Indexes — so
//     idx_server_logs_project/idx_server_logs_env are manual like the
//     baseline's idx_rules_flag), then backfills existing rows by parsing
//     the strict single-space k=v message tokens (project=/key_prefix=/
//     status=/reason=/limiter= prefixes; status parsed to int, skipped
//     on parse failure; empty stays unset). Rows without tokens are
//     skipped, so clean tables rewrite nothing. env is NEVER parsed from
//     messages: message paths hold renameable env slugs, not env IDs, so
//     old rows keep env unset. The backfill saves with SaveNoValidate:
//     project tokens are stored verbatim into the relation field, and
//     dangling IDs (project deleted since logging) are kept as-is —
//     relation validation would otherwise reject them and nulling them
//     would destroy history. Backfill pages by id-keyset cursor (FindRecordsByFilter `id > {:cursor}`,
//     sort "id", LIMIT 500), one short RunInTransaction per page, mirroring
//     1790000023_rename_anonid_bucket. id never changes, so in-place
//     backfills cannot shift the cursor; a rerun converges. Up is
//     idempotent (reruns find fields/indexes present and rewrite the same
//     values).
//   - Down removes the added fields and indexes only (best-effort
//     inverse; stored values are dropped with them). It never drops the
//     collection or touches sibling fields, indexes, or rows.

import (
	"strconv"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

// serverLogsColumns names the filter columns added by this migration.
var serverLogsColumns = []string{"project", "env", "key_prefix", "status", "reason", "limiter"}

// serverLogsColumnIndexes maps each filter column to its non-unique index.
var serverLogsColumnIndexes = [][2]string{
	{"idx_server_logs_project", "project"},
	{"idx_server_logs_env", "env"},
	{"idx_server_logs_key_prefix", "key_prefix"},
	{"idx_server_logs_status", "status"},
	{"idx_server_logs_reason", "reason"},
	{"idx_server_logs_limiter", "limiter"},
}

// serverLogsBackfillPageSize bounds one cursor page: one page = one short
// transaction.
const serverLogsBackfillPageSize = 500

// parseLogMessage extracts the strict single-space k=v filter tokens from a
// persisted message line. Returns project, keyPrefix, status (0 when absent
// or unparseable), statusOK, reason, limiter. Values never contain spaces,
// so splitting on single spaces is exact.
func parseLogMessage(msg string) (project, keyPrefix string, status int, statusOK bool, reason, limiter string) {
	for _, tok := range strings.Split(msg, " ") {
		switch {
		case strings.HasPrefix(tok, "project="):
			project = strings.TrimPrefix(tok, "project=")
		case strings.HasPrefix(tok, "key_prefix="):
			keyPrefix = strings.TrimPrefix(tok, "key_prefix=")
		case strings.HasPrefix(tok, "status="):
			if n, err := strconv.Atoi(strings.TrimPrefix(tok, "status=")); err == nil {
				status, statusOK = n, true
			}
		case strings.HasPrefix(tok, "reason="):
			reason = strings.TrimPrefix(tok, "reason=")
		case strings.HasPrefix(tok, "limiter="):
			limiter = strings.TrimPrefix(tok, "limiter=")
		}
	}
	return project, keyPrefix, status, statusOK, reason, limiter
}

// backfillServerLogsColumns parses every server_logs message and writes the
// extracted filter columns back onto the row, paged by id-keyset cursor so
// heap stays flat and the write lock is never held for the whole table.
// Rows without tokens are skipped (never rewritten). An empty table commits
// nothing and returns nil.
func backfillServerLogsColumns(app core.App) error {
	cursor := ""
	hasCursor := false
	for {
		filter := ""
		params := dbx.Params{}
		if hasCursor {
			filter = "id > {:cursor}"
			params["cursor"] = cursor
		}
		page, err := app.FindRecordsByFilter("server_logs", filter, "id", serverLogsBackfillPageSize, 0, params)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		type dirtyRow struct {
			rec                              *core.Record
			project, keyPrefix, reason       string
			limiter                          string
			status                           int
			hasProject, hasKeyPrefix         bool
			hasStatus, hasReason, hasLimiter bool
		}
		var dirty []dirtyRow
		for _, r := range page {
			project, keyPrefix, status, statusOK, reason, limiter := parseLogMessage(r.GetString("message"))
			d := dirtyRow{rec: r}
			if project != "" {
				d.project, d.hasProject = project, true
			}
			if keyPrefix != "" {
				d.keyPrefix, d.hasKeyPrefix = keyPrefix, true
			}
			if statusOK {
				d.status, d.hasStatus = status, true
			}
			if reason != "" {
				d.reason, d.hasReason = reason, true
			}
			if limiter != "" {
				d.limiter, d.hasLimiter = limiter, true
			}
			if d.hasProject || d.hasKeyPrefix || d.hasStatus || d.hasReason || d.hasLimiter {
				dirty = append(dirty, d)
			}
		}
		last := page[len(page)-1]
		cursor = last.Id
		hasCursor = true
		if len(dirty) > 0 {
			if err := app.RunInTransaction(func(txApp core.App) error {
				for _, d := range dirty {
					if d.hasProject {
						d.rec.Set("project", d.project)
					}
					if d.hasKeyPrefix {
						d.rec.Set("key_prefix", d.keyPrefix)
					}
					if d.hasStatus {
						d.rec.Set("status", d.status)
					}
					if d.hasReason {
						d.rec.Set("reason", d.reason)
					}
					if d.hasLimiter {
						d.rec.Set("limiter", d.limiter)
					}
					// NoValidate keeps dangling project IDs stored
					// verbatim (relation validation would reject them).
					if err := txApp.SaveNoValidate(d.rec); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if len(page) < serverLogsBackfillPageSize {
			break
		}
	}
	return nil
}

func upServerLogsColumns(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("server_logs")
	if err != nil {
		return nil // collection absent; 1790000024 owns creation
	}
	// Resolve the relation target at runtime; the collection id is an
	// internal detail of the squashed baseline and is never hardcoded.
	projects, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		return err
	}
	envs, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		return err
	}
	changed := false
	for _, name := range serverLogsColumns {
		if collection.Fields.GetByName(name) != nil {
			continue
		}
		switch name {
		case "project":
			// Single-select relation to projects without
			// cascade: deleting a project must never wipe
			// log history, so dangling IDs stay stored.
			collection.Fields.Add(&core.RelationField{
				Name:          name,
				CollectionId:  projects.Id,
				MaxSelect:     1,
				CascadeDelete: false,
			})
		case "env":
			// Single-select relation to environments without
			// cascade: deleting an env must never wipe log
			// history, so dangling IDs stay stored. New rows
			// carry the live env ID at write time; old rows
			// keep env unset (slugs in messages are not IDs).
			collection.Fields.Add(&core.RelationField{
				Name:          name,
				CollectionId:  envs.Id,
				MaxSelect:     1,
				CascadeDelete: false,
			})
		case "status":
			collection.Fields.Add(&core.NumberField{Name: name, OnlyInt: true})
		default:
			collection.Fields.Add(&core.TextField{Name: name})
		}
		changed = true
	}
	for _, idx := range serverLogsColumnIndexes {
		if collection.GetIndex(idx[0]) == "" {
			collection.AddIndex(idx[0], false, idx[1], "")
			changed = true
		}
	}
	if changed {
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	return backfillServerLogsColumns(app)
}

func downServerLogsColumns(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("server_logs")
	if err != nil {
		return nil // already gone; keep down idempotent
	}
	changed := false
	for _, idx := range serverLogsColumnIndexes {
		if collection.GetIndex(idx[0]) != "" {
			collection.RemoveIndex(idx[0])
			changed = true
		}
	}
	for _, name := range serverLogsColumns {
		if collection.Fields.GetByName(name) != nil {
			collection.Fields.RemoveByName(name)
			changed = true
		}
	}
	if !changed {
		return nil // already reverted
	}
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		return upServerLogsColumns(app)
	}, func(app core.App) error {
		return downServerLogsColumns(app)
	})
}
