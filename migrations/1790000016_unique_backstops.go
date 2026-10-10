package migrations

// Unique backstops for admin-write identity (ADDITIVE only).
//
// The Go write paths already prevent duplicates in-process (flag-key
// guard, releases writeMu, env-slug mutex), but nothing backs them at
// the DB level: two binaries sharing one SQLite file could fork release
// version numbers or flag identity. This migration upgrades two 0015
// lookup indexes to UNIQUE (same names — AddIndex replaces in place):
//   - releases(env, version)
//   - flags(key, project)
//
// Convergent, never blocking: SQLite refuses a UNIQUE index on tables
// that already hold legacy duplicates, which would fail the whole
// `migrate up` and brick the upgrade. Instead the up step first probes
// each table for duplicate groups; tables with dupes keep their
// non-unique index and get an operator warning naming the offending
// keys (remove the duplicates and re-apply this migration to enforce).
// Clean tables gain the backstop. Down removes the index and restores
// the non-unique 0015 shape; it never touches tables or rows.
//
// environments(project, slug) is deliberately excluded: env writes are
// the rarest path and already carry two guards; the same treatment
// applies on request.

import (
	"fmt"
	"log"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

type uniqueBackstop struct {
	collection string
	name       string
	columns    string
	// dupeExpr is a SQL scalar expression over the table's columns whose
	// value identifies a uniqueness group, NULL-safe via COALESCE and
	// matching the Go comparison semantics (version compares as int,
	// like GetInt).
	dupeExpr string
}

var uniqueBackstops = []uniqueBackstop{
	{"releases", "idx_releases_env_version", "env, version", "COALESCE(env,'') || '|' || COALESCE(CAST(version AS INTEGER),'')"},
	{"flags", "idx_flags_key_project", "key, project", "COALESCE(project,'') || '|' || COALESCE(key,'')"},
}

// findDupeKeys returns one "group (xN)" label per duplicate group
// defined by dupeExpr. Table and dupeExpr are hardcoded per call site
// (never user input).
func findDupeKeys(app core.App, table, dupeExpr string) ([]string, error) {
	var rows []struct {
		K string `db:"k"`
		C int    `db:"c"`
	}
	q := fmt.Sprintf("SELECT (%s) AS k, COUNT(*) AS c FROM %s GROUP BY k HAVING c > 1", dupeExpr, table)
	if err := app.DB().NewQuery(q).All(&rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s (x%d)", r.K, r.C))
	}
	return out, nil
}

// ensureUniqueIndex upgrades collection's name index to UNIQUE when the
// table holds no duplicate groups, reporting whether it did. Tables
// with legacy duplicates keep their non-unique index and get an
// operator warning instead of a failed upgrade.
func ensureUniqueIndex(app core.App, collection, name, columns, dupeExpr string) (bool, error) {
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		return false, nil // collection absent; keep migration idempotent
	}
	dupes, err := findDupeKeys(app, collection, dupeExpr)
	if err != nil {
		return false, err
	}
	if len(dupes) > 0 {
		shown := dupes
		if len(shown) > 5 {
			shown = shown[:5]
		}
		log.Printf("migrations: UNIQUE %s skipped: %d duplicate group(s) in %s (e.g. %s); remove the duplicates and re-apply this migration to enforce",
			name, len(dupes), collection, strings.Join(shown, ", "))
		return false, nil
	}
	col.AddIndex(name, true, columns, "")
	if err := app.Save(col); err != nil {
		return false, err
	}
	return true, nil
}

func applyUniqueBackstops(app core.App, add bool) error {
	for _, idx := range uniqueBackstops {
		col, err := app.FindCollectionByNameOrId(idx.collection)
		if err != nil {
			continue // collection absent; keep migration idempotent
		}
		if add {
			if _, err := ensureUniqueIndex(app, idx.collection, idx.name, idx.columns, idx.dupeExpr); err != nil {
				return err
			}
			continue
		}
		col.RemoveIndex(idx.name)
		col.AddIndex(idx.name, false, idx.columns, "")
		if err := app.Save(col); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	m.Register(func(app core.App) error {
		return applyUniqueBackstops(app, true)
	}, func(app core.App) error {
		return applyUniqueBackstops(app, false)
	})
}
