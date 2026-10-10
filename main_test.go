package main

import (
	"errors"
	"strings"
	"sync"
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

func seedFlag(t *testing.T, app *tests.TestApp, projectID, key string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", projectID)
	rec.Set("key", key)
	rec.Set("type", "bool")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save flag: %v", err)
	}
	return rec
}

func seedRule(t *testing.T, app *tests.TestApp, flagID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("rules")
	if err != nil {
		t.Fatalf("find rules: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("flag", flagID)
	rec.Set("condition", map[string]any{"field": "platform", "op": "==", "value": "android"})
	rec.Set("value", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save rule: %v", err)
	}
	return rec
}

func rulesForFlag(t *testing.T, app *tests.TestApp, flagID string) []*core.Record {
	t.Helper()
	recs, err := app.FindAllRecords("rules")
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	var out []*core.Record
	for _, r := range recs {
		if r.GetString("flag") == flagID {
			out = append(out, r)
		}
	}
	return out
}

// TestFlagDeleteCascadesRules pins the transactional cascade: deleting a
// flag removes the flag and all of its rules, and nothing else's.
func TestFlagDeleteCascadesRules(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-cascade")
	doomed := seedFlag(t, app, pid, "gone_flag")
	seedRule(t, app, doomed.Id)
	seedRule(t, app, doomed.Id)
	keeper := seedFlag(t, app, pid, "kept_flag")
	seedRule(t, app, keeper.Id)

	fresh, err := app.FindRecordById("flags", doomed.Id)
	if err != nil {
		t.Fatalf("reload flag: %v", err)
	}
	if err := app.Delete(fresh); err != nil {
		t.Fatalf("delete flag: unexpected error %v", err)
	}
	if _, err := app.FindRecordById("flags", doomed.Id); err == nil {
		t.Fatal("deleted flag still present")
	}
	if got := rulesForFlag(t, app, doomed.Id); len(got) != 0 {
		t.Fatalf("%d orphan rules left for deleted flag", len(got))
	}
	if got := rulesForFlag(t, app, keeper.Id); len(got) != 1 {
		t.Fatalf("unrelated flag lost rules: %d left, want 1", len(got))
	}
}

// TestFlagDeleteFailureRollsBackRules pins atomicity: when a child rule
// delete fails, the whole cascade rolls back — the flag and all of its
// rules survive for a clean retry.
func TestFlagDeleteFailureRollsBackRules(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-cascade-tx")
	doomed := seedFlag(t, app, pid, "doomed_flag")
	seedRule(t, app, doomed.Id)
	seedRule(t, app, doomed.Id)

	n := 0
	app.OnRecordDelete("rules").BindFunc(func(e *core.RecordEvent) error {
		n++
		if n == 2 {
			return errors.New("cascade-tx-test: forced child delete failure")
		}
		return e.Next()
	})

	fresh, err := app.FindRecordById("flags", doomed.Id)
	if err != nil {
		t.Fatalf("reload flag: %v", err)
	}
	if err := app.Delete(fresh); err == nil {
		t.Fatal("flag delete with failing child delete succeeded, want error")
	}
	if _, err := app.FindRecordById("flags", doomed.Id); err != nil {
		t.Fatalf("flag gone after failed cascade: %v", err)
	}
	if got := rulesForFlag(t, app, doomed.Id); len(got) != 2 {
		t.Fatalf("%d rules left after rollback, want both kept", len(got))
	}
}

// TestFlagDuplicateKeyRejectedByHook pins deterministic flag identity:
// a second flag with the same key in the same project is refused (first-
// match-wins downstream must never be order-dependent), while the same
// key in another project and renames to a free key stay allowed.
func TestFlagDuplicateKeyRejectedByHook(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-dedup")
	seedFlag(t, app, pid, "launch_flag")

	dupCol, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	dup := core.NewRecord(dupCol)
	dup.Set("project", pid)
	dup.Set("key", "launch_flag")
	dup.Set("type", "bool")
	if err := app.Save(dup); err == nil {
		t.Fatal("duplicate flag key saved, want hook refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "already exists") {
		t.Fatalf("save error = %q, want it to mention the duplicate key", err)
	}

	other := seedProject(t, app, "flag-dedup-other")
	seedFlag(t, app, other, "launch_flag")

	free := seedFlag(t, app, pid, "other_flag")
	free.Set("key", "launch_flag")
	if err := app.Save(free); err == nil {
		t.Fatal("flag rename onto a taken key saved, want hook refusal")
	}
}

// TestEnvCreateDuplicateSlugConcurrentOneWinner pins the slug-race fix:
// N concurrent creates of the same (project, slug) yield exactly one
// winner; every loser is refused by the uniqueness check.
func TestEnvCreateDuplicateSlugConcurrentOneWinner(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "slug-race")
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			col, err := app.FindCollectionByNameOrId("environments")
			if err != nil {
				errs[i] = err
				return
			}
			rec := core.NewRecord(col)
			rec.Set("project", pid)
			rec.Set("slug", "dev")
			errs[i] = app.Save(rec)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "already exists") {
			t.Fatalf("loser error = %q, want the slug-taken refusal", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent creates won, want exactly 1", wins)
	}
}
