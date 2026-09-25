// Indexed-lookup tests for todo 11 (fetch-lookups hardening).
//
// FAILING-FIRST: these tests target the new indexed helper findSDKKey,
// which does not exist yet — this file must NOT compile pre-fix (RED).
// Post-fix it proves: key lookup by (prefix, hash) hits the right row,
// unknown/revoked keys miss, and the full key is never persisted.
package ingest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// lookupTestApp builds a TestApp with a minimal sdk_keys collection.
// The env relation is a plain text field holding the env id: the lookup
// only reads it via GetString, exactly like the handler path.
func lookupTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("sdk_keys")
	for _, f := range []string{
		`{"type":"text","name":"prefix"}`,
		`{"type":"text","name":"hash"}`,
		`{"type":"bool","name":"revoked"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"number","name":"rateLimit"}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save sdk_keys collection: %v", err)
	}
	return app
}

func seedKey(t *testing.T, app *tests.TestApp, full, envID string, revoked bool) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("prefix", KeyPrefix(full))
	rec.Set("hash", KeyHash(full))
	rec.Set("revoked", revoked)
	rec.Set("env", envID)
	rec.Set("rateLimit", 60)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save sdk key: %v", err)
	}
}

func requestWithKey(app core.App, full string) *core.RequestEvent {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/env/dev/config", nil)
	if full != "" {
		req.Header.Set(HeaderKey, full)
	}
	re := &core.RequestEvent{App: app}
	re.Request = req
	return re
}

// TestFindSDKKeyIndexed proves the (prefix, hash) lookup: exact row hit,
// unknown key miss, revoked row miss, and hash-only comparison (a row with
// a matching prefix but wrong hash must not match).
func TestFindSDKKeyIndexed(t *testing.T) {
	app := lookupTestApp(t)
	const good, other, revoked = "cw-good-key-0001", "cw-other-key-0002", "cw-revoked-key-03"
	seedKey(t, app, good, "envA", false)
	seedKey(t, app, other, "envB", false)
	seedKey(t, app, revoked, "envA", true)

	rec, err := findSDKKey(app, KeyPrefix(good), KeyHash(good))
	if err != nil {
		t.Fatalf("findSDKKey(good): %v", err)
	}
	if rec == nil || rec.GetString("env") != "envA" {
		t.Fatalf("good key must hit envA row, got %+v", rec)
	}

	if rec, err := findSDKKey(app, KeyPrefix("cw-unknown-xxxx"), KeyHash("cw-unknown-xxxx")); err != nil || rec != nil {
		t.Fatalf("unknown key must miss, got %+v err=%v", rec, err)
	}

	// Same prefix as good, wrong hash: constant-time hash decides, not prefix.
	if rec, err := findSDKKey(app, KeyPrefix(good), KeyHash("cw-good-key-9999")); err != nil || rec != nil {
		t.Fatalf("prefix-only match must miss, got %+v err=%v", rec, err)
	}

	if rec, err := findSDKKey(app, KeyPrefix(revoked), KeyHash(revoked)); err != nil || rec == nil {
		t.Fatalf("revoked row must still be found (revocation is a 401 at the caller), got %+v err=%v", rec, err)
	}
}

// TestRequireSDKKeyAuthOrder proves the handler-level mapping survives
// indexing: unknown/missing/revoked -> error (caller returns 401), and the
// matched record carries the bound env for the scope check downstream.
func TestRequireSDKKeyAuthOrder(t *testing.T) {
	app := lookupTestApp(t)
	const good, revoked = "cw-good-key-0001", "cw-revoked-key-03"
	seedKey(t, app, good, "envA", false)
	seedKey(t, app, revoked, "envA", true)

	rec, err := RequireSDKKey(requestWithKey(app, good))
	if err != nil || rec.GetString("env") != "envA" {
		t.Fatalf("good key must authenticate with envA, got %+v err=%v", rec, err)
	}
	for name, full := range map[string]string{
		"unknown": "cw-unknown-xxxx",
		"missing": "",
		"revoked": revoked,
	} {
		if _, err := RequireSDKKey(requestWithKey(app, full)); err == nil {
			t.Errorf("%s key must fail (401 at handler), got nil error", name)
		}
	}
}
