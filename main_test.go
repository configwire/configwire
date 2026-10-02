package main

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// envGuardTestApp boots a TestApp with the production hooks bound (the
// same registerConfigwireHooks the live server runs). Seeds use the real
// projects/environments collections from migrations, including the real
// cascade relation (environments.project cascades on project delete).
func envGuardTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	registerConfigwireHooks(app)

	if _, err := app.FindCollectionByNameOrId("projects"); err != nil {
		t.Fatalf("find projects: %v", err)
	}
	if _, err := app.FindCollectionByNameOrId("environments"); err != nil {
		t.Fatalf("find environments: %v", err)
	}
	return app
}

func seedProject(t *testing.T, app *tests.TestApp, name string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		t.Fatalf("find projects: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save project: %v", err)
	}
	return rec.Id
}

func seedEnv(t *testing.T, app *tests.TestApp, projectID, slug string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", projectID)
	rec.Set("slug", slug)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env: %v", err)
	}
	return rec
}

func countProjectEnvs(t *testing.T, app *tests.TestApp, projectID string) int {
	t.Helper()
	recs, err := app.FindAllRecords("environments")
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}
	n := 0
	for _, r := range recs {
		if r.GetString("project") == projectID {
			n++
		}
	}
	return n
}

func TestEnvDeleteLastOfProjectBlocked(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "solo")
	only := seedEnv(t, app, pid, "dev")

	if err := app.Delete(only); err == nil {
		t.Fatalf("deleting the last environment succeeded, want refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "last environment") {
		t.Fatalf("delete error = %q, want it to mention the last environment", err)
	}
	if n := countProjectEnvs(t, app, pid); n != 1 {
		t.Fatalf("after blocked delete: %d envs, want the 1 env kept", n)
	}
}

func TestEnvDeleteNonLastAllowedThenLastBlocked(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "pair")
	seedEnv(t, app, pid, "dev")
	other := seedEnv(t, app, pid, "prod")

	fresh, err := app.FindRecordById("environments", other.Id)
	if err != nil {
		t.Fatalf("reload env: %v", err)
	}
	if err := app.Delete(fresh); err != nil {
		t.Fatalf("deleting one of 2 envs: unexpected error %v", err)
	}
	if n := countProjectEnvs(t, app, pid); n != 1 {
		t.Fatalf("after first delete: %d envs, want 1 left", n)
	}

	recs, err := app.FindAllRecords("environments")
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}
	if err := app.Delete(recs[0]); err == nil {
		t.Fatalf("deleting the last environment succeeded, want refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "last environment") {
		t.Fatalf("delete error = %q, want it to mention the last environment", err)
	}
}

func TestProjectDeleteCascadesDespiteLastEnvGuard(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "doomed")
	seedEnv(t, app, pid, "dev")

	proj, err := app.FindRecordById("projects", pid)
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if err := app.Delete(proj); err != nil {
		t.Fatalf("deleting the project: unexpected error %v", err)
	}
	if n := countProjectEnvs(t, app, pid); n != 0 {
		t.Fatalf("after project delete: %d envs, want 0 (cascaded)", n)
	}
}
