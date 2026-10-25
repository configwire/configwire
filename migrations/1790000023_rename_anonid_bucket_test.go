package migrations

// Convention follows 1790000022_drop_identity_userhash_test.go:
// same-package tests calling the unexported helpers in
// 1790000023_rename_anonid_bucket.go directly. tests.NewTestApp boots
// with every registered migration applied, so the app starts in the
// POST-rename state (events.bucket, no events.anonId); the fixture
// reconstructs the pre-rename state via the down helper plus seed rows.
// Style: stdlib testing, Given/When/Then, fresh app per test, no sleep,
// -race/-shuffle safe.

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newPreRenameApp reconstructs the pre-rename schema on top of the fully
// migrated TestApp: the events.anonId column plus one row carrying a
// bucket value ("42"), one unattributed row (""), and country on both.
func newPreRenameApp(t *testing.T) *tests.TestApp {
	t.Helper()

	// Given: a fresh test app (all migrations already applied).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	if err := renameBucketToAnonIdDown(app); err != nil {
		t.Fatalf("fixture renameBucketToAnonIdDown: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	seed := func(kind, anonID, country string) {
		t.Helper()
		rec := core.NewRecord(col)
		rec.Set("kind", kind)
		rec.Set("variant", "control")
		rec.Set("anonId", anonID)
		rec.Set("country", country)
		if err := app.Save(rec); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}
	seed("exposure", "42", "US")
	seed("fetch", "", "FR")
	return app
}

// TestRenameAnonIdToBucketUp pins the rename: values move to bucket,
// empties stay empty, the old column is gone, country is untouched,
// and a rerun converges (idempotent).
func TestRenameAnonIdToBucketUp(t *testing.T) {
	// Given: pre-rename schema with one bucketed and one unattributed row.
	app := newPreRenameApp(t)

	// When: the migration runs.
	if err := renameAnonIdToBucketUp(app); err != nil {
		t.Fatalf("renameAnonIdToBucketUp: %v", err)
	}

	// Then: events.bucket exists, events.anonId is gone.
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	if col.Fields.GetByName("bucket") == nil {
		t.Fatal("events.bucket missing, want present")
	}
	if col.Fields.GetByName("anonId") != nil {
		t.Fatal("events.anonId still present, want removed")
	}

	// And: values moved verbatim, empties stayed empty, country kept.
	recs, err := app.FindAllRecords("events")
	if err != nil || len(recs) != 2 {
		t.Fatalf("want 2 surviving events, got %d (err %v)", len(recs), err)
	}
	byKind := map[string]*core.Record{}
	for _, r := range recs {
		byKind[r.GetString("kind")] = r
	}
	if got := byKind["exposure"].GetString("bucket"); got != "42" {
		t.Errorf("exposure bucket = %q, want moved value %q", got, "42")
	}
	if got := byKind["exposure"].GetString("country"); got != "US" {
		t.Errorf("exposure country = %q, want kept US", got)
	}
	if got := byKind["fetch"].GetString("bucket"); got != "" {
		t.Errorf("fetch bucket = %q, want empty (unattributed kept)", got)
	}
	if got := byKind["fetch"].GetString("country"); got != "FR" {
		t.Errorf("fetch country = %q, want kept FR", got)
	}

	// And: a rerun converges (idempotent, no error).
	if err := renameAnonIdToBucketUp(app); err != nil {
		t.Fatalf("rerun renameAnonIdToBucketUp: %v", err)
	}
}

// TestRenameAnonIdToBucketDownBestEffort pins the inverse: down
// re-adds anonId, copies values back, drops bucket, and is idempotent.
func TestRenameAnonIdToBucketDownBestEffort(t *testing.T) {
	// Given: a fresh test app (post-rename state).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	// When: the down helper runs.
	if err := renameBucketToAnonIdDown(app); err != nil {
		t.Fatalf("renameBucketToAnonIdDown: %v", err)
	}

	// Then: anonId is back, bucket is gone.
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	if col.Fields.GetByName("anonId") == nil {
		t.Fatal("events.anonId not re-added, want present")
	}
	if col.Fields.GetByName("bucket") != nil {
		t.Fatal("events.bucket still present, want removed")
	}

	// And: reruns converge (idempotent, no error).
	if err := renameBucketToAnonIdDown(app); err != nil {
		t.Fatalf("rerun renameBucketToAnonIdDown: %v", err)
	}
}
