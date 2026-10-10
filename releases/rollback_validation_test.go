package releases

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// rollbackSrcRecord builds an unsaved releases row carrying snapshot.
// rollbackToNewRow only reads src, so the corrupt/invalid-shape cases
// need no seeding: they fail before any DB access.
func rollbackSrcRecord(t *testing.T, app *tests.TestApp, snapshot string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", "envA")
	rec.Set("version", 1)
	rec.Set("etag", "abc")
	rec.Set("snapshot", snapshot)
	return rec
}

// TestRollbackRejectsCorruptSnapshot pins that rollback re-validates
// like promote: a non-JSON source snapshot fails instead of cloning
// bad bytes into a new release.
func TestRollbackRejectsCorruptSnapshot(t *testing.T) {
	app := promoteHandlerTestApp(t)
	src := rollbackSrcRecord(t, app, "not-json{{{")
	if _, _, err := rollbackToNewRow(app, src, "note", "tester", 1); err == nil {
		t.Fatal("rollback of corrupt snapshot succeeded, want refusal")
	} else if !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("rollback error = %q, want it to mention corruption", err)
	}
}

// TestRollbackRejectsInvalidSnapshot pins that a well-formed but
// invalid snapshot (one publish and promote would both reject) cannot
// be resurrected through rollback into a new release.
func TestRollbackRejectsInvalidSnapshot(t *testing.T) {
	app := promoteHandlerTestApp(t)
	src := rollbackSrcRecord(t, app, `{"flags":[],"experiments":[]}`)
	if _, _, err := rollbackToNewRow(app, src, "note", "tester", 1); err == nil {
		t.Fatal("rollback of invalid snapshot succeeded, want refusal")
	} else if !strings.Contains(err.Error(), "nothing to publish") {
		t.Fatalf("rollback error = %q, want the ValidateSnapshot refusal", err)
	}
}
