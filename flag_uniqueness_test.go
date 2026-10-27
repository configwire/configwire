package main

import (
	"strings"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// Indexed flag-key uniqueness pre-check (TDD, task T5).
//
// checkFlagKeyTaken must reject a duplicate (project, key) flag via an
// indexed filter (idx_flags_key_project) instead of a full-table scan.
// The Go hook stays authoritative; the migration-16 UNIQUE index is a
// backstop only (convergent: tables with legacy dupes skip enforcement).
// The 400 message "flag key already exists for this project" is unchanged.

// saveFlagWithKey saves one flags row for (projectID, key) through the
// production hooks and returns the save error (nil on success).
func saveFlagWithKey(app *tests.TestApp, projectID, key string) error {
	col, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("project", projectID)
	rec.Set("key", key)
	rec.Set("type", "bool")
	return app.Save(rec)
}

// countProjectKeyFlags counts flags rows for one (project, key) pair.
func countProjectKeyFlags(t *testing.T, app *tests.TestApp, projectID, key string) int {
	t.Helper()
	recs, err := app.FindAllRecords("flags")
	if err != nil {
		t.Fatalf("list flags: %v", err)
	}
	n := 0
	for _, r := range recs {
		if r.GetString("project") == projectID && r.GetString("key") == key {
			n++
		}
	}
	return n
}

// TestFlagKeyTakenDuplicateCreateRejected pins the deterministic 400: a
// second flag with the same key in the same project is refused.
func TestFlagKeyTakenDuplicateCreateRejected(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-unique-create")
	seedFlag(t, app, pid, "launch_flag")

	if err := saveFlagWithKey(app, pid, "launch_flag"); err == nil {
		t.Fatal("duplicate flag key saved, want hook refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "flag key already exists for this project") {
		t.Fatalf("save error = %q, want the flag-key-taken 400", err)
	}
	if n := countProjectKeyFlags(t, app, pid, "launch_flag"); n != 1 {
		t.Fatalf("%d rows for (project,key), want exactly 1", n)
	}
}

// TestFlagKeyTakenUpdateOntoTakenKeyRejected pins update-path coverage:
// renaming a different flag onto a taken key is refused.
func TestFlagKeyTakenUpdateOntoTakenKeyRejected(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-unique-update")
	seedFlag(t, app, pid, "taken_flag")
	other := seedFlag(t, app, pid, "free_flag")

	fresh, err := app.FindRecordById("flags", other.Id)
	if err != nil {
		t.Fatalf("reload flag: %v", err)
	}
	fresh.Set("key", "taken_flag")
	if err := app.Save(fresh); err == nil {
		t.Fatal("flag rename onto a taken key saved, want hook refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "flag key already exists for this project") {
		t.Fatalf("save error = %q, want the flag-key-taken 400", err)
	}
}

// TestFlagKeyTakenCrossProjectAllowed pins project scoping: the same key
// in a different project saves fine.
func TestFlagKeyTakenCrossProjectAllowed(t *testing.T) {
	app := envGuardTestApp(t)
	pidA := seedProject(t, app, "flag-unique-xa")
	pidB := seedProject(t, app, "flag-unique-xb")
	seedFlag(t, app, pidA, "shared_flag")
	if err := saveFlagWithKey(app, pidB, "shared_flag"); err != nil {
		t.Fatalf("same key in another project refused: %v", err)
	}
}

// TestFlagKeyTakenUpdateKeepingOwnKeyAllowed pins the self-exclusion
// (id != {:id}): touching a flag without changing its key saves fine.
func TestFlagKeyTakenUpdateKeepingOwnKeyAllowed(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-unique-ownkey")
	mine := seedFlag(t, app, pid, "stable_flag")

	fresh, err := app.FindRecordById("flags", mine.Id)
	if err != nil {
		t.Fatalf("reload flag: %v", err)
	}
	fresh.Set("key", "stable_flag")
	if err := app.Save(fresh); err != nil {
		t.Fatalf("flag update keeping its own key refused: %v", err)
	}
}

// TestFlagKeyTakenConcurrentDoubleCreateOneWinner pins the race bound:
// N concurrent creates of the same (project, key) yield exactly one row.
// Sequential losers are refused by the Go hook 400; racers that pass the
// check together are stopped by the UNIQUE backstop — either way exactly
// one wins and every loser errors.
func TestFlagKeyTakenConcurrentDoubleCreateOneWinner(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-unique-race")
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = saveFlagWithKey(app, pid, "race_flag")
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		lowered := strings.ToLower(err.Error())
		if !strings.Contains(lowered, "already exists") && !strings.Contains(lowered, "unique") {
			t.Fatalf("loser error = %q, want the flag-key-taken refusal or the UNIQUE backstop", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent creates won, want exactly 1", wins)
	}
	if got := countProjectKeyFlags(t, app, pid, "race_flag"); got != 1 {
		t.Fatalf("%d rows for (project,key) after race, want exactly 1", got)
	}
}

// TestCheckFlagKeyShapeFiresFirst pins hook order: an empty key is
// rejected by shape validation before the uniqueness pre-check runs.
func TestCheckFlagKeyShapeFiresFirst(t *testing.T) {
	app := envGuardTestApp(t)
	pid := seedProject(t, app, "flag-unique-shape")
	if err := saveFlagWithKey(app, pid, ""); err == nil {
		t.Fatal("empty flag key saved, want shape refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "invalid flag key") {
		t.Fatalf("save error = %q, want the shape validation to fire first", err)
	}
}
