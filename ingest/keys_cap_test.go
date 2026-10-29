package ingest

import (
	"strings"
	"testing"
)

func clearKeysCapEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "")
}

func TestMaxKeysPerEnvUnsetIsUnlimited(t *testing.T) {
	clearKeysCapEnv(t)
	if got := maxKeysPerEnv(); got != 0 {
		t.Fatalf("maxKeysPerEnv unset = %d, want 0 (unlimited)", got)
	}
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	for i := 0; i < 5; i++ {
		if _, _, err := mintKey(app, env, 0, 0); err != nil {
			t.Fatalf("mintKey %d: %v", i, err)
		}
	}
	if msg, err := keyCapError(app, env); err != nil || msg != "" {
		t.Fatalf("uncapped env must never hit the cap, got msg=%q err=%v", msg, err)
	}
}

func TestMaxKeysPerEnvMalformedIsUnlimited(t *testing.T) {
	for _, raw := range []string{"", "  ", "abc", "3.5", "-1", "-100"} {
		t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", raw)
		if got := maxKeysPerEnv(); got != 0 {
			t.Fatalf("maxKeysPerEnv(%q) = %d, want 0 (unlimited)", raw, got)
		}
	}
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "not-a-number")
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	for i := 0; i < 4; i++ {
		if _, _, err := mintKey(app, env, 0, 0); err != nil {
			t.Fatalf("mintKey %d: %v", i, err)
		}
	}
	if msg, err := keyCapError(app, env); err != nil || msg != "" {
		t.Fatalf("malformed cap must mean unlimited, got msg=%q err=%v", msg, err)
	}
}

func TestKeyCapAtCapRejectsNamingCap(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "2")
	if got := maxKeysPerEnv(); got != 2 {
		t.Fatalf("maxKeysPerEnv = %d, want 2", got)
	}
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	if msg, err := keyCapError(app, env); err != nil || msg != "" {
		t.Fatalf("empty env must be under cap, got msg=%q err=%v", msg, err)
	}
	if _, _, err := mintKey(app, env, 0, 0); err != nil {
		t.Fatalf("mintKey 1: %v", err)
	}
	if msg, err := keyCapError(app, env); err != nil || msg != "" {
		t.Fatalf("1 of 2 must be under cap, got msg=%q err=%v", msg, err)
	}
	if _, _, err := mintKey(app, env, 0, 0); err != nil {
		t.Fatalf("mintKey 2: %v", err)
	}
	msg, err := keyCapError(app, env)
	if err != nil {
		t.Fatalf("keyCapError: %v", err)
	}
	if msg == "" {
		t.Fatal("2 of 2 must hit the cap")
	}
	if !strings.Contains(msg, "max 2") {
		t.Fatalf("cap message must name the cap, got %q", msg)
	}
}

func TestKeyCapRevokedExcluded(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "2")
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	rec1, _, err := mintKey(app, env, 0, 0)
	if err != nil {
		t.Fatalf("mintKey 1: %v", err)
	}
	if _, _, err := mintKey(app, env, 0, 0); err != nil {
		t.Fatalf("mintKey 2: %v", err)
	}
	if msg, err := keyCapError(app, env); err != nil || msg == "" {
		t.Fatalf("2 active of 2 must hit the cap, got msg=%q err=%v", msg, err)
	}
	rec1.Set("revoked", true)
	if err := app.Save(rec1); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if msg, err := keyCapError(app, env); err != nil || msg != "" {
		t.Fatalf("revoked key must not count (1 active of 2), got msg=%q err=%v", msg, err)
	}
	if _, _, err := mintKey(app, env, 0, 0); err != nil {
		t.Fatalf("mint after revoke must succeed: %v", err)
	}
	if msg, err := keyCapError(app, env); err != nil || msg == "" {
		t.Fatalf("back at 2 active of 2 must hit the cap, got msg=%q err=%v", msg, err)
	}
}

func TestKeyCapOneEdge(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "1")
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	if _, _, err := mintKey(app, env, 0, 0); err != nil {
		t.Fatalf("mintKey: %v", err)
	}
	msg, err := keyCapError(app, env)
	if err != nil {
		t.Fatalf("keyCapError: %v", err)
	}
	if !strings.Contains(msg, "max 1") {
		t.Fatalf("cap=1 message must name the cap, got %q", msg)
	}
}

func TestKeyCapMissingRevokedCountsActive(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "1")
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	rec, _, err := mintKey(app, env, 0, 0)
	if err != nil {
		t.Fatalf("mintKey: %v", err)
	}
	if rec.GetBool("revoked") {
		t.Fatal("freshly minted row must read as not revoked")
	}
	if n, err := activeKeyCount(app, env); err != nil || n != 1 {
		t.Fatalf("legacy-shaped row (revoked never set) must count as active, got n=%d err=%v", n, err)
	}
}

func TestKeyCapScopedPerEnv(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_KEYS_PER_ENV", "1")
	app := keyVerTestApp(t)
	envA := seedEnv(t, app)
	envB := seedEnv(t, app)
	if _, _, err := mintKey(app, envA, 0, 0); err != nil {
		t.Fatalf("mintKey envA: %v", err)
	}
	if msg, err := keyCapError(app, envA); err != nil || msg == "" {
		t.Fatalf("envA at 1 of 1 must hit the cap, got msg=%q err=%v", msg, err)
	}
	if msg, err := keyCapError(app, envB); err != nil || msg != "" {
		t.Fatalf("envB must have its own budget, got msg=%q err=%v", msg, err)
	}
}
