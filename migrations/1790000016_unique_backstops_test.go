package migrations

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func uniqueIndexSQL(t *testing.T, app *tests.TestApp, collection, name string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatalf("find %s: %v", collection, err)
	}
	return col.GetIndex(name)
}

// TestUniqueBackstopsApplied pins that clean tables gain UNIQUE indexes
// after migrations.
func TestUniqueBackstopsApplied(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	for _, idx := range uniqueBackstops {
		sql := uniqueIndexSQL(t, app, idx.collection, idx.name)
		if sql == "" {
			t.Errorf("%s: index %q missing after migration", idx.collection, idx.name)
		} else if !strings.Contains(strings.ToUpper(sql), "UNIQUE") {
			t.Errorf("%s: index %q is not UNIQUE: %s", idx.collection, idx.name, sql)
		}
	}
}

// TestUniqueBackstopSkipsOnDuplicates pins the convergent path: a table
// with legacy duplicates keeps working (no UNIQUE, no error), and gains
// the UNIQUE backstop once the dupes are removed.
func TestUniqueBackstopSkipsOnDuplicates(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("t_uniq_probe")
	col.Fields.Add(&core.TextField{Name: "code"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save probe collection: %v", err)
	}
	mk := func(code string) {
		t.Helper()
		rec := core.NewRecord(col)
		rec.Set("code", code)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save probe row: %v", err)
		}
	}
	mk("a")
	mk("a")
	mk("b")

	applied, err := ensureUniqueIndex(app, "t_uniq_probe", "idx_t_uniq_probe", "code", "COALESCE(code,'')")
	if err != nil {
		t.Fatalf("ensure with dupes: unexpected error %v", err)
	}
	if applied {
		t.Fatal("UNIQUE applied over duplicate groups, want skip")
	}
	if sql := uniqueIndexSQL(t, app, "t_uniq_probe", "idx_t_uniq_probe"); strings.Contains(strings.ToUpper(sql), "UNIQUE") {
		t.Fatalf("UNIQUE index present despite dupes: %s", sql)
	}

	recs, err := app.FindAllRecords("t_uniq_probe")
	if err != nil {
		t.Fatalf("list probe rows: %v", err)
	}
	if err := app.Delete(recs[0]); err != nil {
		t.Fatalf("delete dupe: %v", err)
	}
	applied, err = ensureUniqueIndex(app, "t_uniq_probe", "idx_t_uniq_probe", "code", "COALESCE(code,'')")
	if err != nil {
		t.Fatalf("ensure after cleanup: unexpected error %v", err)
	}
	if !applied {
		t.Fatal("UNIQUE not applied after dupe cleanup, want applied")
	}
	if sql := uniqueIndexSQL(t, app, "t_uniq_probe", "idx_t_uniq_probe"); !strings.Contains(strings.ToUpper(sql), "UNIQUE") {
		t.Fatalf("index is not UNIQUE after cleanup: %s", sql)
	}
}
