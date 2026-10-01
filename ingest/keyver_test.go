package ingest

import (
	"strings"
	"testing"

	"github.com/configwire/configwire/limits"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func keyVerTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	keys := core.NewBaseCollection("sdk_keys")
	for _, f := range []string{
		`{"type":"text","name":"prefix"}`,
		`{"type":"text","name":"hash"}`,
		`{"type":"text","name":"verifier"}`,
		`{"type":"number","name":"keyVer","onlyInt":true}`,
		`{"type":"bool","name":"revoked"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"number","name":"fetchRps","onlyInt":true}`,
		`{"type":"number","name":"ingestRps","onlyInt":true}`,
	} {
		if err := keys.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add sdk_keys field %s: %v", f, err)
		}
	}
	if err := app.Save(keys); err != nil {
		t.Fatalf("save sdk_keys collection: %v", err)
	}
	envs := core.NewBaseCollection("environments")
	if err := app.Save(envs); err != nil {
		t.Fatalf("save environments collection: %v", err)
	}
	return app
}

func seedEnv(t *testing.T, app *tests.TestApp) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env: %v", err)
	}
	return rec.Id
}

func seedLegacyKey(t *testing.T, app *tests.TestApp, full, envID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("prefix", KeyPrefix(full))
	rec.Set("hash", KeyHash(full))
	rec.Set("env", envID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save legacy key: %v", err)
	}
	return rec
}

func seedV2Key(t *testing.T, app *tests.TestApp, full, envID string) *core.Record {
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
	rec.Set("env", envID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save v2 key: %v", err)
	}
	return rec
}

func TestGenerateKeyShape(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	b, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	for _, k := range []string{a, b} {
		if len(k) != 27 || !strings.HasPrefix(k, "cw-") {
			t.Fatalf("key shape = %q, want cw- + 24 base62 chars", k)
		}
		for _, c := range k[3:] {
			if !strings.ContainsRune(keyAlphabet, c) {
				t.Fatalf("key %q has non-base62 rune %q", k, c)
			}
		}
	}
	if a == b {
		t.Fatal("two generated keys must differ")
	}
}

func TestVerifierRoundTrip(t *testing.T) {
	full, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	v, err := GenerateVerifier(full)
	if err != nil {
		t.Fatalf("GenerateVerifier: %v", err)
	}
	if !strings.HasPrefix(v, "$2a$") {
		t.Fatalf("verifier = %q, want $2a$ bcrypt prefix", v)
	}
	if !VerifySlow(v, full) {
		t.Fatal("correct key must verify")
	}
	if VerifySlow(v, full+"x") {
		t.Fatal("wrong key must not verify")
	}
}

func TestLegacyRowStillAuthenticates(t *testing.T) {
	app := keyVerTestApp(t)
	ResetKeyCache()
	env := seedEnv(t, app)
	const full = "cw-legacy-key-0001-abcd"
	seedLegacyKey(t, app, full, env)
	rec, err := findSDKKey(app, full)
	if err != nil || rec == nil {
		t.Fatalf("legacy row must authenticate, got %+v err=%v", rec, err)
	}
	if _, err := RequireSDKKey(requestWithKey(app, full)); err != nil {
		t.Fatalf("RequireSDKKey(legacy): %v", err)
	}
}

func TestV2RowAuthMatrix(t *testing.T) {
	app := keyVerTestApp(t)
	ResetKeyCache()
	env := seedEnv(t, app)
	otherEnv := seedEnv(t, app)
	full, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	rec := seedV2Key(t, app, full, env)
	if got := rec.GetString("verifier"); !strings.HasPrefix(got, "$2a$") {
		t.Fatalf("stored verifier = %q, want $2a$ bcrypt", got)
	}
	if got := rec.GetString("hash"); got != "" {
		t.Fatalf("v2 row must store hash empty, got %q", got)
	}
	if _, err := RequireSDKKey(requestWithKey(app, full)); err != nil {
		t.Fatalf("correct v2 key must authenticate: %v", err)
	}
	ResetKeyCache()
	if _, err := RequireSDKKey(requestWithKey(app, full+"zz")); err == nil {
		t.Fatal("wrong v2 key must be denied")
	}
	rec.Set("revoked", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := RequireSDKKey(requestWithKey(app, full)); err == nil {
		t.Fatal("revoked v2 key must be denied immediately (no stale cache auth)")
	}
	rec.Set("revoked", false)
	if err := app.Save(rec); err != nil {
		t.Fatalf("un-revoke: %v", err)
	}
	ResetKeyCache()
	other, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	seedV2Key(t, app, other, otherEnv)
	got, err := findSDKKey(app, other)
	if err != nil || got == nil {
		t.Fatalf("other-env key must authenticate, got %+v err=%v", got, err)
	}
	if got.GetString("env") != otherEnv {
		t.Fatalf("row env = %q, want %q (env scope rides on this binding)", got.GetString("env"), otherEnv)
	}
	if got.GetString("env") == env {
		t.Fatal("cross-env key must stay bound to its own env")
	}
}

func TestV2CacheHotPath(t *testing.T) {
	app := keyVerTestApp(t)
	ResetKeyCache()
	env := seedEnv(t, app)
	full, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	seedV2Key(t, app, full, env)
	if _, err := RequireSDKKey(requestWithKey(app, full)); err != nil {
		t.Fatalf("first auth: %v", err)
	}
	if n := SlowVerifyCalls(); n != 1 {
		t.Fatalf("first request must slow-verify once, got %d", n)
	}
	if _, err := RequireSDKKey(requestWithKey(app, full)); err != nil {
		t.Fatalf("second auth: %v", err)
	}
	if n := SlowVerifyCalls(); n != 1 {
		t.Fatalf("second request must be a cache hit (zero new bcrypt calls), got %d", n)
	}
}

func TestV2DeletedRowEvicted(t *testing.T) {
	app := keyVerTestApp(t)
	ResetKeyCache()
	env := seedEnv(t, app)
	full, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	rec := seedV2Key(t, app, full, env)
	if _, err := RequireSDKKey(requestWithKey(app, full)); err != nil {
		t.Fatalf("pre-delete auth: %v", err)
	}
	if err := app.Delete(rec); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := RequireSDKKey(requestWithKey(app, full)); err == nil {
		t.Fatal("deleted row must be denied with cache evicted")
	}
}

func TestMintKeyServerGenerated(t *testing.T) {
	app := keyVerTestApp(t)
	ResetKeyCache()
	env := seedEnv(t, app)
	rec, full, err := mintKey(app, env, 0, 0)
	if err != nil {
		t.Fatalf("mintKey: %v", err)
	}
	if !strings.HasPrefix(full, "cw-") || len(full) != 27 {
		t.Fatalf("minted key shape = %q", full)
	}
	if got := rec.GetString("verifier"); !strings.HasPrefix(got, "$2a$") {
		t.Fatalf("stored verifier = %q, want $2a$", got)
	}
	if got := rec.GetString("hash"); got != "" {
		t.Fatalf("minted row must store hash empty, got %q", got)
	}
	if got := rec.GetInt("keyVer"); got != 2 {
		t.Fatalf("keyVer = %d, want 2", got)
	}
	rec2, full2, err := mintKey(app, env, 5, 7)
	if err != nil {
		t.Fatalf("mintKey 2: %v", err)
	}
	if full2 == full {
		t.Fatal("two minted keys must differ")
	}
	if got := rec2.GetInt("fetchRps"); got != 5 {
		t.Fatalf("fetchRps = %d, want 5", got)
	}
	if _, err := RequireSDKKey(requestWithKey(app, full)); err != nil {
		t.Fatalf("minted key 1 must authenticate: %v", err)
	}
	ResetKeyCache()
	if _, err := RequireSDKKey(requestWithKey(app, full2)); err != nil {
		t.Fatalf("minted key 2 must authenticate: %v", err)
	}
}

func TestMintInputValidation(t *testing.T) {
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	if _, _, ok := mintInputError(app, "", 0, 0); ok {
		t.Fatal("missing env must be rejected")
	}
	if st, _, ok := mintInputError(app, "", 0, 0); ok || st != 404 {
		t.Fatalf("missing env must be 404, got status=%d ok=%v", st, ok)
	}
	if st, _, ok := mintInputError(app, "no-such-env", 0, 0); ok || st != 404 {
		t.Fatalf("unknown env must be 404, got status=%d ok=%v", st, ok)
	}
	for _, bad := range []int{-1, 10001} {
		if st, _, ok := mintInputError(app, env, bad, 0); ok || st != 400 {
			t.Fatalf("fetchRps=%d must be 400, got status=%d ok=%v", bad, st, ok)
		}
		if st, _, ok := mintInputError(app, env, 0, bad); ok || st != 400 {
			t.Fatalf("ingestRps=%d must be 400, got status=%d ok=%v", bad, st, ok)
		}
	}
	if _, _, ok := mintInputError(app, env, 0, 0); !ok {
		t.Fatal("empty budgets must be valid (unset)")
	}
	if _, _, ok := mintInputError(app, env, 1, 10000); !ok {
		t.Fatal("1..10000 budgets must be valid")
	}
}

func TestBucketsKeyedByID(t *testing.T) {
	a := core.NewBaseCollection("sdk_keys")
	rec1 := core.NewRecord(a)
	rec1.Id = "key-id-1"
	rec2 := core.NewRecord(a)
	rec2.Id = "key-id-2"
	if rec1.Id == rec2.Id {
		t.Fatal("test needs distinct record ids")
	}
	if !limits.AllowFetchKey(rec1.Id, 1) {
		t.Fatal("first fetch hit for key 1 must pass")
	}
	if limits.AllowFetchKey(rec1.Id, 1) {
		t.Fatal("second fetch hit for key 1 must be limited")
	}
	if !limits.AllowFetchKey(rec2.Id, 1) {
		t.Fatal("key 2 must have an independent fetch bucket")
	}
	if !limits.AllowIngestKey(rec1.Id, 1) {
		t.Fatal("first ingest hit for key 1 must pass")
	}
	if limits.AllowIngestKey(rec1.Id, 1) {
		t.Fatal("second ingest hit for key 1 must be limited")
	}
	if !limits.AllowIngestKey(rec2.Id, 1) {
		t.Fatal("key 2 must have an independent ingest bucket")
	}
}
