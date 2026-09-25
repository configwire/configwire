// Indexed-lookup tests for todo 11 (fetch-lookups hardening).
//
// BEHAVIOR-PRESERVATION: latestRelease already exists, so this file
// compiles pre-fix and pins current behavior (latest = max version per
// env, nil when the env has no release). It stays green through the
// indexed rewrite: same rows in, same row out — only the query changes.
package fetch

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// lookupTestApp builds a TestApp with a minimal releases collection.
// Only the fields latestRelease reads are modeled (env + version);
// snapshot/etag shapes are covered by the pure EvaluateSnapshot tests.
func lookupTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("releases")
	for _, f := range []string{
		`{"type":"text","name":"env"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"text","name":"etag"}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save releases collection: %v", err)
	}
	return app
}

func seedRelease(t *testing.T, app *tests.TestApp, envID string, version int, etag string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", envID)
	rec.Set("version", version)
	rec.Set("etag", etag)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save release: %v", err)
	}
}

// TestLatestReleaseIsMaxVersionPerEnv pins: the served row is the env's
// max version; other envs' rows never leak in; an env with no row yields
// nil (the handler serves the {version:0, etag:none} empty release).
func TestLatestReleaseIsMaxVersionPerEnv(t *testing.T) {
	app := lookupTestApp(t)
	seedRelease(t, app, "envA", 1, "etag-a1")
	seedRelease(t, app, "envA", 3, "etag-a3")
	seedRelease(t, app, "envA", 2, "etag-a2")
	seedRelease(t, app, "envB", 9, "etag-b9")

	rel, err := latestRelease(app, "envA")
	if err != nil {
		t.Fatalf("latestRelease(envA): %v", err)
	}
	if rel == nil {
		t.Fatal("envA has releases, got nil")
	}
	if got := rel.GetInt("version"); got != 3 {
		t.Fatalf("latest version = %d, want 3", got)
	}
	if got := rel.GetString("etag"); got != "etag-a3" {
		t.Fatalf("latest etag = %q, want etag-a3 (stored bytes served verbatim)", got)
	}

	rel, err = latestRelease(app, "envB")
	if err != nil || rel == nil || rel.GetInt("version") != 9 {
		t.Fatalf("envB must resolve version 9, got %+v err=%v", rel, err)
	}

	rel, err = latestRelease(app, "envEmpty")
	if err != nil {
		t.Fatalf("empty env must not error, got %v", err)
	}
	if rel != nil {
		t.Fatalf("empty env must yield nil (empty-release path), got version %d", rel.GetInt("version"))
	}
}
