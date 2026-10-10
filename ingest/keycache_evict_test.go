package ingest

import (
	"testing"
	"time"
)

func TestEvictKeyByIDRemovesOnlyTarget(t *testing.T) {
	ResetKeyCache()
	cachePut("fast-a", "id-1")
	cachePut("fast-b", "id-2")
	cachePut("fast-c", "id-1")

	EvictKeyByID("id-1")

	if _, ok := cacheGet("fast-a"); ok {
		t.Fatal("fast-a (id-1) must be evicted")
	}
	if _, ok := cacheGet("fast-c"); ok {
		t.Fatal("fast-c (id-1) must be evicted")
	}
	if id, ok := cacheGet("fast-b"); !ok || id != "id-2" {
		t.Fatalf("fast-b (id-2) must survive, got id=%q ok=%v", id, ok)
	}

	EvictKeyByID("")
	EvictKeyByID("no-such-id")
	if id, ok := cacheGet("fast-b"); !ok || id != "id-2" {
		t.Fatalf("empty/unknown evict must be a no-op, got id=%q ok=%v", id, ok)
	}
	ResetKeyCache()
}

// TestFastCacheTTLExpires pins cross-process revocation convergence: a
// cached entry stops hitting after the TTL with no eviction hook firing
// (hooks only reach the local process).
func TestFastCacheTTLExpires(t *testing.T) {
	old := fastCacheTTL
	fastCacheTTL = 100 * time.Millisecond
	defer func() { fastCacheTTL = old }()
	ResetKeyCache()
	cachePut("fast-ttl", "id-ttl")
	if id, ok := cacheGet("fast-ttl"); !ok || id != "id-ttl" {
		t.Fatalf("fresh entry must hit, got id=%q ok=%v", id, ok)
	}
	time.Sleep(300 * time.Millisecond)
	if _, ok := cacheGet("fast-ttl"); ok {
		t.Fatal("expired entry must miss")
	}
	ResetKeyCache()
}

func TestVerifierRotationEvictsStaleKey(t *testing.T) {
	app := keyVerTestApp(t)
	RegisterKeyCacheHooks(app)
	ResetKeyCache()
	env := seedEnv(t, app)
	oldFull, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	rec := seedV2Key(t, app, oldFull, env)
	if _, err := RequireSDKKey(requestWithKey(app, oldFull)); err != nil {
		t.Fatalf("pre-rotation auth: %v", err)
	}
	if n := SlowVerifyCalls(); n != 1 {
		t.Fatalf("pre-rotation auth must slow-verify once, got %d", n)
	}

	newFull, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	newVerifier, err := GenerateVerifier(newFull)
	if err != nil {
		t.Fatalf("GenerateVerifier: %v", err)
	}
	rec.Set("verifier", newVerifier)
	rec.Set("prefix", KeyPrefix(newFull))
	if err := app.Save(rec); err != nil {
		t.Fatalf("rotate verifier: %v", err)
	}

	if _, err := RequireSDKKey(requestWithKey(app, oldFull)); err == nil {
		t.Fatal("old key must 401 after in-place verifier rotation (hook evicted)")
	}
	if _, err := RequireSDKKey(requestWithKey(app, newFull)); err != nil {
		t.Fatalf("new key must authenticate: %v", err)
	}
}

func TestDeleteHookEvictsStaleKey(t *testing.T) {
	app := keyVerTestApp(t)
	RegisterKeyCacheHooks(app)
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
		t.Fatal("deleted key must 401 after delete hook evicted the cache")
	}
}

func TestCachedAuthStillSkipsBcrypt(t *testing.T) {
	app := keyVerTestApp(t)
	RegisterKeyCacheHooks(app)
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
		t.Fatalf("second (cached) auth: %v", err)
	}
	if n := SlowVerifyCalls(); n != 1 {
		t.Fatalf("second request must be a cache hit with zero new bcrypt calls, got %d", n)
	}
}
