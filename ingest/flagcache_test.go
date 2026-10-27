package ingest

import (
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// flagCacheTestApp builds a TestApp with a minimal flags collection
// (project + key text fields), mirroring the production filter
// "project = {:p} && key = {:k}" used by resolveFlagIDs.
func flagCacheTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	flags := core.NewBaseCollection("flags")
	for _, f := range []string{
		`{"type":"text","name":"project"}`,
		`{"type":"text","name":"key"}`,
	} {
		if err := flags.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add flags field %s: %v", f, err)
		}
	}
	if err := app.Save(flags); err != nil {
		t.Fatalf("save flags collection: %v", err)
	}
	return app
}

func seedFlag(t *testing.T, app *tests.TestApp, project, key string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", project)
	rec.Set("key", key)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save flag %s/%s: %v", project, key, err)
	}
	return rec
}

// TestFlagIDCacheRerunZeroDBHits: second identical batch issues 0
// flag-lookup DB hits (counter hook).
func TestFlagIDCacheRerunZeroDBHits(t *testing.T) {
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()
	seedFlag(t, app, "projA", "flagX")
	seedFlag(t, app, "projA", "flagY")

	events := []EventIn{{Kind: "fetch", Flag: "flagX"}, {Kind: "fetch", Flag: "flagY"}}

	before := FlagLookupCalls()
	resolveFlagIDs(app, "projA", events)
	firstHits := FlagLookupCalls() - before
	if firstHits != 2 {
		t.Fatalf("first batch must issue 2 lookups, got %d", firstHits)
	}

	before = FlagLookupCalls()
	got := resolveFlagIDs(app, "projA", events)
	secondHits := FlagLookupCalls() - before
	if secondHits != 0 {
		t.Fatalf("second identical batch must issue 0 DB hits, got %d", secondHits)
	}
	if got["flagX"] == "" || got["flagY"] == "" {
		t.Fatalf("cached IDs must be non-empty, got %v", got)
	}
}

// TestResolveFlagIDsUnknownThenCreate: unknown key negative-cached as ""
// then resolved after flag creation (create-hook eviction).
func TestResolveFlagIDsUnknownThenCreate(t *testing.T) {
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()

	events := []EventIn{{Kind: "exposure", Flag: "newbie"}}
	got := resolveFlagIDs(app, "projA", events)
	if got["newbie"] != "" {
		t.Fatalf("unknown key must resolve to \"\", got %q", got["newbie"])
	}
	// Negative hit must also be cached: rerun costs 0 lookups.
	before := FlagLookupCalls()
	got = resolveFlagIDs(app, "projA", events)
	if n := FlagLookupCalls() - before; n != 0 {
		t.Fatalf("unknown-key rerun must be a negative-cache hit (0 lookups), got %d", n)
	}

	rec := seedFlag(t, app, "projA", "newbie")
	got = resolveFlagIDs(app, "projA", events)
	if got["newbie"] != rec.Id {
		t.Fatalf("after create, unknown key must attribute to %q, got %q", rec.Id, got["newbie"])
	}
}

// TestFlagIDCacheProjectIsolation: projA/flagX != projB/flagX.
func TestFlagIDCacheProjectIsolation(t *testing.T) {
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()
	recA := seedFlag(t, app, "projA", "flagX")
	recB := seedFlag(t, app, "projB", "flagX")
	if recA.Id == recB.Id {
		t.Fatal("test needs distinct flag rows per project")
	}

	events := []EventIn{{Kind: "fetch", Flag: "flagX"}}
	gotA := resolveFlagIDs(app, "projA", events)
	gotB := resolveFlagIDs(app, "projB", events)
	if gotA["flagX"] != recA.Id {
		t.Fatalf("projA/flagX must attribute to %q, got %q", recA.Id, gotA["flagX"])
	}
	if gotB["flagX"] != recB.Id {
		t.Fatalf("projB/flagX must attribute to %q, got %q", recB.Id, gotB["flagX"])
	}
	if gotA["flagX"] == gotB["flagX"] {
		t.Fatal("cross-project same key must yield distinct IDs")
	}
}

// TestFlagIDCacheUpdateEvicts: updating a flag's key evicts the stale entry.
func TestFlagIDCacheUpdateEvicts(t *testing.T) {
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()
	rec := seedFlag(t, app, "projA", "flagX")

	events := []EventIn{{Kind: "fetch", Flag: "flagX"}}
	got := resolveFlagIDs(app, "projA", events)
	if got["flagX"] != rec.Id {
		t.Fatalf("pre-update must attribute to %q, got %q", rec.Id, got["flagX"])
	}

	rec.Set("key", "flagX2")
	if err := app.Save(rec); err != nil {
		t.Fatalf("rename flag: %v", err)
	}

	got = resolveFlagIDs(app, "projA", []EventIn{{Kind: "fetch", Flag: "flagX"}})
	if got["flagX"] != "" {
		t.Fatalf("old key must miss (\"\") after rename eviction, got %q", got["flagX"])
	}
	got = resolveFlagIDs(app, "projA", []EventIn{{Kind: "fetch", Flag: "flagX2"}})
	if got["flagX2"] != rec.Id {
		t.Fatalf("new key must attribute to %q, got %q", rec.Id, got["flagX2"])
	}
}

// TestFlagIDCacheDeleteEvicts: deleting a flag evicts so lookups fall
// back to env-wide ("").
func TestFlagIDCacheDeleteEvicts(t *testing.T) {
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()
	rec := seedFlag(t, app, "projA", "flagX")

	events := []EventIn{{Kind: "fetch", Flag: "flagX"}}
	got := resolveFlagIDs(app, "projA", events)
	if got["flagX"] != rec.Id {
		t.Fatalf("pre-delete must attribute to %q, got %q", rec.Id, got["flagX"])
	}
	if err := app.Delete(rec); err != nil {
		t.Fatalf("delete flag: %v", err)
	}
	got = resolveFlagIDs(app, "projA", events)
	if got["flagX"] != "" {
		t.Fatalf("deleted flag must resolve to \"\" (env-wide fallback), got %q", got["flagX"])
	}
}

// TestFlagIDCacheTTLExpires: expired entries re-hit the DB. Shortens the
// TTL in-test only; the production constant stays 60s.
func TestFlagIDCacheTTLExpires(t *testing.T) {
	old := flagCacheTTL
	flagCacheTTL = 100 * time.Millisecond
	defer func() { flagCacheTTL = old }()
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()
	seedFlag(t, app, "projA", "flagX")

	events := []EventIn{{Kind: "fetch", Flag: "flagX"}}
	resolveFlagIDs(app, "projA", events)
	before := FlagLookupCalls()
	resolveFlagIDs(app, "projA", events)
	if n := FlagLookupCalls() - before; n != 0 {
		t.Fatalf("fresh entry must hit (0 lookups), got %d", n)
	}
	time.Sleep(300 * time.Millisecond)
	before = FlagLookupCalls()
	resolveFlagIDs(app, "projA", events)
	if n := FlagLookupCalls() - before; n != 1 {
		t.Fatalf("expired entry must re-lookup once, got %d", n)
	}
}

// TestResolveFlagIDsEmptyFlagNoLookup: empty-flag events never touch the DB.
func TestResolveFlagIDsEmptyFlagNoLookup(t *testing.T) {
	app := flagCacheTestApp(t)
	RegisterFlagCacheHooks(app)
	ResetFlagCache()
	before := FlagLookupCalls()
	got := resolveFlagIDs(app, "projA", []EventIn{{Kind: "fetch"}, {Kind: "exposure", Flag: ""}})
	if n := FlagLookupCalls() - before; n != 0 {
		t.Fatalf("empty-flag events must issue 0 lookups, got %d", n)
	}
	if len(got) != 0 {
		t.Fatalf("empty-flag events must yield no mappings, got %v", got)
	}
}
