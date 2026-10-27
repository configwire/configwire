package migrations

// Convention follows 1790000008_drop_event_flag_test.go: same-package
// tests calling the unexported helpers in
// 1790000022_drop_identity_userhash.go directly. tests.NewTestApp boots
// with every registered migration applied, so the app starts in the
// POST-drop state (no identity_salts collection, no events.userHash
// field); the fixture reconstructs the pre-drop state via the down
// helpers plus one seed event row. Style: stdlib testing,
// Given/When/Then, fresh app per test, no sleep, -race/-shuffle safe.

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// newPreDropApp reconstructs the pre-drop schema on top of the fully
// migrated TestApp: the identity_salts collection, the events.userHash
// column, and one event row carrying userHash + bucket + country.
func newPreDropApp(t *testing.T) *tests.TestApp {
	t.Helper()

	// Given: a fresh test app (all migrations already applied).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	if err := addIdentitySaltsDown(app); err != nil {
		t.Fatalf("fixture addIdentitySaltsDown: %v", err)
	}
	if err := addIdentityStorageDown(app); err != nil {
		t.Fatalf("fixture addIdentityStorageDown: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("kind", "exposure")
	rec.Set("variant", "control")
	rec.Set("userHash", "legacy-hash-must-go")
	rec.Set("bucket", "42")
	rec.Set("country", "US")
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return app
}

// TestDropIdentityStorageUp pins the drop: the collection is deleted,
// the column is removed, the seed row survives with bucket/country
// intact, and a rerun converges (idempotent).
func TestDropIdentityStorageUp(t *testing.T) {
	// Given: pre-drop schema with one dirty row.
	app := newPreDropApp(t)

	// When: the migration runs.
	if err := dropIdentityStorageUp(app); err != nil {
		t.Fatalf("dropIdentityStorageUp: %v", err)
	}

	// Then: identity_salts is gone.
	if _, err := app.FindCollectionByNameOrId("identity_salts"); err == nil {
		t.Fatal("identity_salts still present, want deleted")
	}

	// And: events.userHash is gone.
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	if col.Fields.GetByName("userHash") != nil {
		t.Fatal("events.userHash still present, want removed")
	}

	// And: the row survives with bucket/country intact.
	recs, err := app.FindAllRecords("events")
	if err != nil || len(recs) != 1 {
		t.Fatalf("want 1 surviving event, got %d (err %v)", len(recs), err)
	}
	if got := recs[0].GetString("bucket"); got != "42" {
		t.Errorf("bucket = %q, want kept bucket %q", got, "42")
	}
	if got := recs[0].GetString("country"); got != "US" {
		t.Errorf("country = %q, want kept US", got)
	}

	// And: a rerun converges (idempotent, no error).
	if err := dropIdentityStorageUp(app); err != nil {
		t.Fatalf("rerun dropIdentityStorageUp: %v", err)
	}
}

// TestDropIdentityStorageDownBestEffort pins the inverse: down
// re-creates the collection and re-adds the column (values not
// restored), and is idempotent.
func TestDropIdentityStorageDownBestEffort(t *testing.T) {
	// Given: a fresh test app (post-drop state).
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	// When: the down helpers run.
	if err := addIdentitySaltsDown(app); err != nil {
		t.Fatalf("addIdentitySaltsDown: %v", err)
	}
	if err := addIdentityStorageDown(app); err != nil {
		t.Fatalf("addIdentityStorageDown: %v", err)
	}

	// Then: both exist again.
	if _, err := app.FindCollectionByNameOrId("identity_salts"); err != nil {
		t.Fatalf("identity_salts not re-created: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	if col.Fields.GetByName("userHash") == nil {
		t.Fatal("events.userHash not re-added, want present")
	}

	// And: reruns converge (idempotent, no error).
	if err := addIdentitySaltsDown(app); err != nil {
		t.Fatalf("rerun addIdentitySaltsDown: %v", err)
	}
	if err := addIdentityStorageDown(app); err != nil {
		t.Fatalf("rerun addIdentityStorageDown: %v", err)
	}
}
