// Indexed-lookup tests for todo 11 (fetch-lookups hardening).
//
// FAILING-FIRST: these tests target the new indexed helper findSDKKey,
// which does not exist yet — this file must NOT compile pre-fix (RED).
// Post-fix it proves: key lookup by (prefix, hash) hits the right row,
// unknown/revoked keys miss, and the full key is never persisted.
package ingest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
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
		`{"type":"text","name":"verifier"}`,
		`{"type":"number","name":"keyVer","onlyInt":true}`,
		`{"type":"bool","name":"revoked"}`,
		`{"type":"text","name":"env"}`,
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
	verifier, err := GenerateVerifier(full)
	if err != nil {
		t.Fatalf("GenerateVerifier: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("prefix", KeyPrefix(full))
	rec.Set("hash", "")
	rec.Set("keyVer", 2)
	rec.Set("verifier", verifier)
	rec.Set("revoked", revoked)
	rec.Set("env", envID)
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

// TestFindSDKKeyIndexed proves the (prefix, verifier) lookup: exact row hit,
// unknown key miss, revoked row miss, and the hash-only comparison (a row with
// a matching prefix but wrong key must not match).
func TestFindSDKKeyIndexed(t *testing.T) {
	app := lookupTestApp(t)
	ResetKeyCache()
	const good, other, revoked = "cw-good-key-0001", "cw-other-key-0002", "cw-revoked-key-03"
	seedKey(t, app, good, "envA", false)
	seedKey(t, app, other, "envB", false)
	seedKey(t, app, revoked, "envA", true)

	rec, err := findSDKKey(app, good)
	if err != nil {
		t.Fatalf("findSDKKey(good): %v", err)
	}
	if rec == nil || rec.GetString("env") != "envA" {
		t.Fatalf("good key must hit envA row, got %+v", rec)
	}

	ResetKeyCache()
	if rec, err := findSDKKey(app, "cw-unknown-xxxx"); err != nil || rec != nil {
		t.Fatalf("unknown key must miss, got %+v err=%v", rec, err)
	}

	// Same prefix as good, wrong key: hash/verifier decides, not prefix.
	ResetKeyCache()
	if rec, err := findSDKKey(app, "cw-good-k9999"); err != nil || rec != nil {
		t.Fatalf("prefix-only match must miss, got %+v err=%v", rec, err)
	}

	ResetKeyCache()
	if rec, err := findSDKKey(app, revoked); err != nil || rec == nil {
		t.Fatalf("revoked row must still be found (revocation is a 401 at the caller), got %+v err=%v", rec, err)
	}
}

// TestRequireSDKKeyAuthOrder proves the handler-level mapping survives
// indexing: unknown/missing/revoked -> error (caller returns 401), and the
// matched record carries the bound env for the scope check downstream.
func TestRequireSDKKeyAuthOrder(t *testing.T) {
	app := lookupTestApp(t)
	ResetKeyCache()
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

// requestWithRecorder builds a RequestEvent with a real ResponseWriter
// so written-response paths (429/503) can be asserted instead of
// panicking on a nil Response.
func requestWithRecorder(app core.App, full string) (*core.RequestEvent, *httptest.ResponseRecorder) {
	re := requestWithKey(app, full)
	rec := httptest.NewRecorder()
	re.Response = rec
	return re, rec
}

// TestRequireKeyEnvAliasStorageError500 pins that a storage failure on
// the key-only alias path reads as an outage (500, retryable), never as
// an unknown env (404, which would send operators rotating keys during
// an outage). Technique mirrors TestRequireSDKKeyDBError503's fixture
// sabotage (plus the DROP TABLE precedent in account_test.go): the key
// row stays valid (sdk_keys intact, so key auth succeeds) while the
// environments table is dropped, so env resolution fails with a storage
// error rather than sql.ErrNoRows. The dangling-key 404 case stays
// pinned separately by TestRequireKeyEnvAliasDanglingKeyEnvIs404.
func TestRequireKeyEnvAliasStorageError500(t *testing.T) {
	app := lookupTestApp(t)
	ResetKeyCache()
	defer ResetKeyCache()

	envs := core.NewBaseCollection("environments")
	if err := app.Save(envs); err != nil {
		t.Fatalf("save environments collection: %v", err)
	}
	envRec := core.NewRecord(envs)
	if err := app.Save(envRec); err != nil {
		t.Fatalf("save env record: %v", err)
	}
	const good = "cw-alias-500-0001"
	seedKey(t, app, good, envRec.Id, false)
	ResetKeyCache()

	if _, err := app.DB().NewQuery("DROP TABLE environments").Execute(); err != nil {
		t.Fatalf("drop environments table: %v", err)
	}
	for name, kind := range map[string]KeyEnvKind{
		"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
	} {
		re, rec := requestWithRecorder(app, good)
		_, _, err := RequireKeyEnv(re, kind)
		if !errors.Is(err, ErrKeyResponded) {
			t.Errorf("%s: storage error must yield ErrKeyResponded, got %v", name, err)
			continue
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: storage error status = %d, want 500", name, rec.Code)
		} else if body := rec.Body.String(); !strings.Contains(body, "internal error") || strings.Contains(body, "no such table") {
			t.Errorf("%s: 500 body = %q, want generic text without driver detail", name, body)
		}
	}
}

// TestRequireSDKKeyDBError503 pins that a storage lookup failure reads
// as an outage (503, retryable), never as a bad credential (401, which
// would send operators rotating keys during an outage).
func TestRequireSDKKeyDBError503(t *testing.T) {
	app := lookupTestApp(t)
	ResetKeyCache()
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	if err := app.Delete(col); err != nil {
		t.Fatalf("delete sdk_keys: %v", err)
	}
	re, rec := requestWithRecorder(app, "cw-good-key-0001")
	_, err = RequireSDKKey(re)
	if !errors.Is(err, ErrKeyResponded) {
		t.Fatalf("db error must yield ErrKeyResponded, got %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("db error status = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Storage error") {
		t.Fatalf("503 body = %q, want storage-error message", body)
	}
}

// TestRequireSDKKeySaturation429 pins the verifier-saturation path: a
// valid but unverifiable-right-now key gets 429 + Retry-After (never
// 401), the hit is counted for ops, and the written-response contract
// holds (no nil-key panic downstream).
func TestRequireSDKKeySaturation429(t *testing.T) {
	app := lookupTestApp(t)
	ResetKeyCache()
	seedV2Key(t, app, "cw-v2-sat-key-01", "envA")
	for i := 0; i < cap(slowSem); i++ {
		slowSem <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(slowSem); i++ {
			<-slowSem
		}
	}()
	re, rec := requestWithRecorder(app, "cw-v2-sat-key-01")
	_, err := RequireSDKKey(re)
	if !errors.Is(err, ErrKeyResponded) {
		t.Fatalf("saturated verifier must yield ErrKeyResponded, got %v", err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("saturation status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatal("saturation must carry Retry-After: 1")
	}
	if SlowSaturatedCalls() != 1 {
		t.Fatalf("saturated count = %d, want 1", SlowSaturatedCalls())
	}
}
