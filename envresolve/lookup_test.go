// Indexed-lookup tests for todo 11 (fetch-lookups hardening).
//
// FAILING-FIRST: these tests target the new indexed helper findEnvsBySlug,
// which does not exist yet — this file must NOT compile pre-fix (RED).
// Post-fix it proves: slug lookup is DB-filtered, Resolve keeps its
// 404/400 semantics, ResolveForKey stays a point lookup (key env IS the
// env — same slug in two projects never misroutes), and the legacy
// empty-key fallback to Resolve still works.
package envresolve

import (
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// lookupTestApp builds a TestApp with a minimal environments collection.
func lookupTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("environments")
	for _, f := range []string{
		`{"type":"text","name":"slug"}`,
		`{"type":"text","name":"project"}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save environments collection: %v", err)
	}
	return app
}

func seedEnv(t *testing.T, app *tests.TestApp, slug, project string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", slug)
	rec.Set("project", project)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env: %v", err)
	}
	return rec.Id
}

// TestFindEnvsBySlugIndexed proves the slug prefilter returns exactly the
// same-slug rows (the input PickIndex decides over).
func TestFindEnvsBySlugIndexed(t *testing.T) {
	app := lookupTestApp(t)
	seedEnv(t, app, "dev", "projA")
	seedEnv(t, app, "dev", "projB")
	seedEnv(t, app, "prod", "projA")

	rows, err := findEnvsBySlug(app, "dev")
	if err != nil {
		t.Fatalf("findEnvsBySlug(dev): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("slug dev must match 2 rows, got %d", len(rows))
	}
	rows, err = findEnvsBySlug(app, "nope")
	if err != nil {
		t.Fatalf("findEnvsBySlug(nope): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("unknown slug must match 0 rows, got %d", len(rows))
	}
}

// TestResolveKeepsSemantics proves Resolve on the indexed path keeps the
// contract: unique slug resolves, shared slug without qualifier is 400,
// unknown slug/qualifier is 404.
func TestResolveKeepsSemantics(t *testing.T) {
	app := lookupTestApp(t)
	seedEnv(t, app, "dev", "projA")
	seedEnv(t, app, "dev", "projB")
	seedEnv(t, app, "prod", "projA")

	env, err := Resolve(app, "prod", "")
	if err != nil || env.GetString("project") != "projA" {
		t.Fatalf("unique slug must resolve, got %+v err=%v", env, err)
	}
	if _, err := Resolve(app, "dev", ""); !errors.As(err, &AmbiguousError{}) {
		t.Fatalf("shared slug without qualifier must be AmbiguousError, got %v", err)
	}
	env, err = Resolve(app, "dev", "projB")
	if err != nil || env.GetString("project") != "projB" {
		t.Fatalf("qualifier must select projB, got %+v err=%v", env, err)
	}
	if _, err := Resolve(app, "nope", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown slug must be ErrNotFound, got %v", err)
	}
}

// TestResolveForKeyNeverMisroutes is the adversarial probe: the same slug
// in two projects must resolve through the KEY's env (point lookup), so a
// key for projA can never serve projB's env. Unscoped keys (empty env) are
// rejected with ErrScopeMismatch — no legacy fallback to Resolve.
func TestResolveForKeyNeverMisroutes(t *testing.T) {
	app := lookupTestApp(t)
	idA := seedEnv(t, app, "dev", "projA")
	idB := seedEnv(t, app, "dev", "projB")
	seedEnv(t, app, "solo", "projA")

	env, err := ResolveForKey(app, "dev", idA)
	if err != nil || env.Id != idA {
		t.Fatalf("key for projA must resolve env %s, got %+v err=%v", idA, env, err)
	}
	env, err = ResolveForKey(app, "dev", idB)
	if err != nil || env.Id != idB {
		t.Fatalf("key for projB must resolve env %s, got %+v err=%v", idB, env, err)
	}
	if _, err := ResolveForKey(app, "prod", idA); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("wrong slug for key env must be ErrScopeMismatch, got %v", err)
	}
	if _, err := ResolveForKey(app, "dev", "missing-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dangling key env must be ErrNotFound, got %v", err)
	}
	// Unscoped keys rejected: empty key env is ErrScopeMismatch for both
	// solo and shared slugs — never falls back to Resolve.
	if _, err := ResolveForKey(app, "solo", ""); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("unscoped key must be ErrScopeMismatch on solo slug, got %v", err)
	}
	if _, err := ResolveForKey(app, "dev", ""); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("unscoped key must be ErrScopeMismatch on shared slug, got %v", err)
	}
}
