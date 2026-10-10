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

func TestCheckRuleRegexConditions(t *testing.T) {
	if err := checkRuleRegexConditions(nil); err != nil {
		t.Fatalf("nil condition should pass (shape is publish's gate): %v", err)
	}
	valid := map[string]any{"field": "platform", "op": "regex", "value": "^and"}
	if err := checkRuleRegexConditions(valid); err != nil {
		t.Fatalf("valid regex condition rejected: %v", err)
	}
	validArr := []any{
		map[string]any{"field": "platform", "op": "==", "value": "android"},
		map[string]any{"field": "country", "op": "regex", "value": "US$"},
	}
	if err := checkRuleRegexConditions(validArr); err != nil {
		t.Fatalf("valid regex array rejected: %v", err)
	}
	notRegexOp := map[string]any{"field": "platform", "op": "==", "value": "([not-a-regex"}
	if err := checkRuleRegexConditions(notRegexOp); err != nil {
		t.Fatalf("non-regex op must not validate the value as regex: %v", err)
	}
	bad := map[string]any{"field": "platform", "op": "regex", "value": "([bad"}
	if err := checkRuleRegexConditions(bad); err == nil {
		t.Fatal("uncompilable regex condition should be rejected")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "invalid regex") {
		t.Fatalf("error = %q, want it to mention invalid regex", err)
	}
	big := map[string]any{"field": "platform", "op": "regex", "value": strings.Repeat("a", 300)}
	if err := checkRuleRegexConditions(big); err == nil {
		t.Fatal("oversized regex condition should be rejected")
	}
	if err := checkRuleRegexConditions("platform==android"); err == nil {
		t.Fatal("non-object condition should be rejected")
	}
}

func TestRuleSaveBadRegexRejectedByHook(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "rules-guard")
	flagCol, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	frec := core.NewRecord(flagCol)
	frec.Set("project", pid)
	frec.Set("key", "launch_flag")
	frec.Set("type", "bool")
	if err := app.Save(frec); err != nil {
		t.Fatalf("save flag: %v", err)
	}
	ruleCol, err := app.FindCollectionByNameOrId("rules")
	if err != nil {
		t.Fatalf("find rules: %v", err)
	}
	bad := core.NewRecord(ruleCol)
	bad.Set("flag", frec.Id)
	bad.Set("condition", map[string]any{"field": "platform", "op": "regex", "value": "([bad"})
	bad.Set("value", true)
	if err := app.Save(bad); err == nil {
		t.Fatal("rule with uncompilable regex saved, want hook refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "invalid regex") {
		t.Fatalf("save error = %q, want it to mention invalid regex", err)
	}
	good := core.NewRecord(ruleCol)
	good.Set("flag", frec.Id)
	good.Set("condition", map[string]any{"field": "platform", "op": "==", "value": "android"})
	good.Set("value", true)
	if err := app.Save(good); err != nil {
		t.Fatalf("valid rule should save: %v", err)
	}
	good.Set("condition", map[string]any{"field": "platform", "op": "regex", "value": "([bad"})
	if err := app.Save(good); err == nil {
		t.Fatal("rule update to uncompilable regex saved, want hook refusal")
	}
}
