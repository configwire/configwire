package ingest

// Alias-route tests for RequireKeyEnv (key-derived env resolution) plus
// legacy-slug parity: key-only SDK routes carry no {env} segment and
// resolve purely from the key's bound env, while legacy
// /api/v1/env/{env}/* routes validate the slug against the key.
// A client-sent ?env= query value of any shape is accepted-but-ignored
// (never read, never validated).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/configwire/configwire/internal/testhelp"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func keyenvAliasApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	envs := core.NewBaseCollection("environments")
	for _, f := range []string{
		`{"type":"text","name":"slug"}`,
		`{"type":"text","name":"project"}`,
	} {
		if err := envs.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add environments field %s: %v", f, err)
		}
	}
	if err := app.Save(envs); err != nil {
		t.Fatalf("save environments collection: %v", err)
	}

	keys := core.NewBaseCollection("sdk_keys")
	for _, f := range []string{
		`{"type":"text","name":"prefix"}`,
		`{"type":"text","name":"hash"}`,
		`{"type":"text","name":"verifier"}`,
		`{"type":"number","name":"keyVer","onlyInt":true}`,
		`{"type":"bool","name":"revoked"}`,
		`{"type":"text","name":"env"}`,
	} {
		if err := keys.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add sdk_keys field %s: %v", f, err)
		}
	}
	if err := app.Save(keys); err != nil {
		t.Fatalf("save sdk_keys collection: %v", err)
	}

	ResetKeyCache()
	t.Cleanup(ResetKeyCache)
	return app
}

func seedKeyenvEnv(t *testing.T, app *tests.TestApp, slug string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", slug)
	rec.Set("project", "p1")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env %s: %v", slug, err)
	}
	return rec.Id
}

func seedKeyenvKey(t *testing.T, app *tests.TestApp, full, envID string) string {
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
	rec.Set("revoked", false)
	rec.Set("env", envID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save sdk key: %v", err)
	}
	return rec.Id
}

// keyenvRequest builds a RequestEvent for both SDK route forms: an empty
// slug (no PathValue) models the key-only aliases, a non-empty slug
// models the legacy /api/v1/env/{env}/* routes. The path may carry a
// ?env= query value (accepted-but-ignored by handlers).
func keyenvRequest(app core.App, method, path, slug, key string) (*core.RequestEvent, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	if slug != "" {
		req.SetPathValue("env", slug)
	}
	if key != "" {
		req.Header.Set(HeaderKey, key)
	}
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	return re, rec
}

// TestRequireKeyEnvResolvesFromKey pins the key-only contract: with a
// valid key RequireKeyEnv resolves the key's bound env for every
// rate-limit kind the handlers use.
func TestRequireKeyEnvResolvesFromKey(t *testing.T) {
	app := keyenvAliasApp(t)
	envDev := seedKeyenvEnv(t, app, "dev")
	seedKeyenvEnv(t, app, "other")
	const goodKey = "cw-alias-good-0001"
	seedKeyenvKey(t, app, goodKey, envDev)

	kinds := map[string]KeyEnvKind{
		"fetch":  KindFetch,
		"ingest": KindIngest,
		"stream": KindStream,
	}
	for name, kind := range kinds {
		reAlias, _ := keyenvRequest(app, http.MethodGet, "/api/v1/config", "", goodKey)
		aliasKey, aliasEnv, err := RequireKeyEnv(reAlias, kind)
		if err != nil {
			t.Fatalf("%s alias: RequireKeyEnv err = %v", name, err)
		}
		if aliasEnv.Id != envDev {
			t.Errorf("%s: alias env = %q, want %q", name, aliasEnv.Id, envDev)
		}
		if aliasKey.Id == "" {
			t.Errorf("%s: alias key id is empty (same key must authenticate)", name)
		}
	}
}

// TestRequireKeyEnvAliasUnknownKeyIs401 pins that the key check still runs
// first on key-only routes: an unknown key 401s before any env resolution.
func TestRequireKeyEnvAliasUnknownKeyIs401(t *testing.T) {
	app := keyenvAliasApp(t)
	seedKeyenvEnv(t, app, "dev")

	for name, kind := range map[string]KeyEnvKind{
		"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
	} {
		re, rec := keyenvRequest(app, http.MethodGet, "/api/v1/config", "", "cw-alias-unknown-xxxx")
		_, _, err := RequireKeyEnv(re, kind)
		if code, msg := testhelp.CodeMessage(err, rec); code != http.StatusUnauthorized {
			t.Errorf("%s: alias unknown-key status = %d, want 401", name, code)
		} else if msg != "Missing or invalid SDK key." {
			t.Errorf("%s: alias unknown-key message = %q, want the 401 key shape", name, msg)
		}
	}
}

// TestRequireKeyEnvAliasDanglingKeyEnvIs404 pins that a key bound to a
// nonexistent env id 404s on the key-only route with the
// "Unknown env." shape.
func TestRequireKeyEnvAliasDanglingKeyEnvIs404(t *testing.T) {
	app := keyenvAliasApp(t)
	seedKeyenvEnv(t, app, "dev")
	seedKeyenvKey(t, app, "cw-alias-dangling-02", "missing-env-id")

	for name, kind := range map[string]KeyEnvKind{
		"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
	} {
		re, rec := keyenvRequest(app, http.MethodGet, "/api/v1/config", "", "cw-alias-dangling-02")
		_, _, err := RequireKeyEnv(re, kind)
		if code, msg := testhelp.CodeMessage(err, rec); code != http.StatusNotFound {
			t.Errorf("%s: alias dangling-env status = %d, want 404", name, code)
		} else if msg != "Unknown env." {
			t.Errorf("%s: alias dangling-env message = %q, want the 404 env shape", name, msg)
		}
	}
}

// TestRequireKeyEnvAliasEmptyKeyEnvIs401 pins that an unscoped key (empty
// bound env) 401s on the key-only route with the scope-mismatch shape.
func TestRequireKeyEnvAliasEmptyKeyEnvIs401(t *testing.T) {
	app := keyenvAliasApp(t)
	seedKeyenvEnv(t, app, "dev")
	seedKeyenvKey(t, app, "cw-alias-unscoped-03", "")

	for name, kind := range map[string]KeyEnvKind{
		"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
	} {
		re, rec := keyenvRequest(app, http.MethodGet, "/api/v1/config", "", "cw-alias-unscoped-03")
		_, _, err := RequireKeyEnv(re, kind)
		if code, msg := testhelp.CodeMessage(err, rec); code != http.StatusUnauthorized {
			t.Errorf("%s: alias unscoped-key status = %d, want 401", name, code)
		} else if msg != "SDK key is not authorized for this env." {
			t.Errorf("%s: alias unscoped-key message = %q, want the scope-mismatch shape", name, msg)
		}
	}
}

// TestRequireKeyEnvLegacySlugValidated pins the dual-serve legacy
// contract: a non-empty {env} slug is validated against the key's bound
// env — matching slug resolves, mismatched slug 401s (wrong-env).
func TestRequireKeyEnvLegacySlugValidated(t *testing.T) {
	app := keyenvAliasApp(t)
	envDev := seedKeyenvEnv(t, app, "dev")
	seedKeyenvEnv(t, app, "other")
	const goodKey = "cw-alias-scope-0004"
	seedKeyenvKey(t, app, goodKey, envDev)

	for name, kind := range map[string]KeyEnvKind{
		"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
	} {
		reOK, _ := keyenvRequest(app, http.MethodGet, "/api/v1/env/dev/config", "dev", goodKey)
		_, okEnv, err := RequireKeyEnv(reOK, kind)
		if err != nil {
			t.Errorf("%s: matching slug must resolve, got %v", name, err)
		} else if okEnv.Id != envDev {
			t.Errorf("%s: matching-slug env = %q, want %q", name, okEnv.Id, envDev)
		}
		reBad, recBad := keyenvRequest(app, http.MethodGet, "/api/v1/env/other/config", "other", goodKey)
		_, _, err = RequireKeyEnv(reBad, kind)
		if code, msg := testhelp.CodeMessage(err, recBad); code != http.StatusUnauthorized {
			t.Errorf("%s: mismatched slug status = %d, want 401", name, code)
		} else if msg != "SDK key is not authorized for this env." {
			t.Errorf("%s: mismatched slug message = %q, want scope-mismatch shape", name, msg)
		}
	}
}

// TestRequireKeyEnvLegacyAliasParity pins that the legacy slug route and
// the key-only alias resolve to the same env for a valid key.
func TestRequireKeyEnvLegacyAliasParity(t *testing.T) {
	app := keyenvAliasApp(t)
	envDev := seedKeyenvEnv(t, app, "dev")
	const goodKey = "cw-alias-parity-0006"
	seedKeyenvKey(t, app, goodKey, envDev)

	for name, kind := range map[string]KeyEnvKind{
		"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
	} {
		reLegacy, _ := keyenvRequest(app, http.MethodGet, "/api/v1/env/dev/config", "dev", goodKey)
		_, legacyEnv, err := RequireKeyEnv(reLegacy, kind)
		if err != nil {
			t.Fatalf("%s legacy: RequireKeyEnv err = %v", name, err)
		}
		reAlias, _ := keyenvRequest(app, http.MethodGet, "/api/v1/config", "", goodKey)
		_, aliasEnv, err := RequireKeyEnv(reAlias, kind)
		if err != nil {
			t.Fatalf("%s alias: RequireKeyEnv err = %v", name, err)
		}
		if legacyEnv.Id != aliasEnv.Id {
			t.Errorf("%s: legacy env %q != alias env %q", name, legacyEnv.Id, aliasEnv.Id)
		}
	}
}

// callPostEvents invokes postEvents on either SDK route form: an empty
// slug (no PathValue) models the key-only alias, a non-empty slug models
// the legacy /api/v1/env/{env}/* route.
func callPostEvents(app core.App, path, slug, key, body string) (int, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if slug != "" {
		req.SetPathValue("env", slug)
	}
	if key != "" {
		req.Header.Set(HeaderKey, key)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postEvents(re); err != nil {
		return testhelp.Code(err, rec), rec
	}
	return rec.Code, rec
}

// TestPostEventsLegacyAliasParity pins that the key-only alias serves the
// same 202 shape as the legacy route for a valid key and minimal batch,
// while a mismatched legacy slug still 401s (wrong-env).
func TestPostEventsLegacyAliasParity(t *testing.T) {
	app := keyenvAliasApp(t)
	envDev := seedKeyenvEnv(t, app, "dev")
	seedKeyenvEnv(t, app, "other")
	const goodKey = "cw-alias-ingest-202-0008"
	seedKeyenvKey(t, app, goodKey, envDev)

	old := module.batcher
	module.batcher = NewBatcher(app)
	defer func() { module.batcher = old }()

	const batch = `{"events":[{"kind":"fetch"}]}`
	legacyCode, legacyRec := callPostEvents(app, "/api/v1/env/dev/events", "dev", goodKey, batch)
	if legacyCode != http.StatusAccepted {
		t.Fatalf("legacy ingest: status = %d, want 202", legacyCode)
	}
	aliasCode, aliasRec := callPostEvents(app, "/api/v1/events", "", goodKey, batch)
	if aliasCode != http.StatusAccepted {
		t.Fatalf("alias ingest: status = %d, want 202", aliasCode)
	}
	var legacyBody, aliasBody map[string]any
	if err := json.Unmarshal(legacyRec.Body.Bytes(), &legacyBody); err != nil {
		t.Fatalf("legacy ingest body is not JSON: %v", err)
	}
	if err := json.Unmarshal(aliasRec.Body.Bytes(), &aliasBody); err != nil {
		t.Fatalf("alias ingest body is not JSON: %v", err)
	}
	if legacyBody["accepted"] != aliasBody["accepted"] || legacyBody["accepted"] != float64(1) {
		t.Errorf("legacy vs alias bodies differ: %v vs %v, want accepted 1", legacyBody, aliasBody)
	}
	if code, _ := callPostEvents(app, "/api/v1/env/other/events", "other", goodKey, batch); code != http.StatusUnauthorized {
		t.Errorf("legacy wrong-env: status = %d, want 401", code)
	}
}

// TestPostEventsEnvQueryStampedOnBodyError pins that a client-sent ?env=
// stamps X-CW-Deprecated-env even when the request then fails body
// validation: the stamp runs right after key+env resolution, before
// body/bucket checks.
func TestPostEventsEnvQueryStampedOnBodyError(t *testing.T) {
	app := keyenvAliasApp(t)
	envDev := seedKeyenvEnv(t, app, "dev")
	const goodKey = "cw-alias-envhdr-0007"
	seedKeyenvKey(t, app, goodKey, envDev)

	for _, c := range []struct {
		name       string
		path       string
		wantHeader bool
	}{
		{"env-with-bad-body", "/api/v1/events?env=dev", true},
		{"no-env-bad-body", "/api/v1/events", false},
	} {
		req := httptest.NewRequest(http.MethodPost, c.path, nil)
		req.Header.Set(HeaderKey, goodKey)
		rec := httptest.NewRecorder()
		re := &core.RequestEvent{App: app}
		re.Request = req
		re.Response = rec
		_ = postEvents(re)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.name, rec.Code)
			continue
		}
		if got := rec.Header().Get(envCompatHeader); (got == "true") != c.wantHeader {
			t.Errorf("%s: deprecated-env header = %q, want-header = %v", c.name, got, c.wantHeader)
		}
	}
}

// TestRequireKeyEnvIgnoresEnvQuery pins that a client-sent ?env= query
// value of any shape (valid slug, wrong slug, garbage) is
// accepted-but-ignored: resolution succeeds purely on the key.
func TestRequireKeyEnvIgnoresEnvQuery(t *testing.T) {
	app := keyenvAliasApp(t)
	envDev := seedKeyenvEnv(t, app, "dev")
	seedKeyenvEnv(t, app, "other")
	const goodKey = "cw-alias-envq-0005"
	seedKeyenvKey(t, app, goodKey, envDev)

	queries := []string{"dev", "other", "no-such-env", "../../../etc", "", "   "}
	for _, q := range queries {
		path := "/api/v1/config?env=" + url.QueryEscape(q)
		for name, kind := range map[string]KeyEnvKind{
			"fetch": KindFetch, "ingest": KindIngest, "stream": KindStream,
		} {
			re, _ := keyenvRequest(app, http.MethodGet, path, "", goodKey)
			_, env, err := RequireKeyEnv(re, kind)
			if err != nil {
				t.Errorf("%s ?env=%q: must be ignored, got %v", name, q, err)
			} else if env.Id != envDev {
				t.Errorf("%s ?env=%q: env = %q, want %q", name, q, env.Id, envDev)
			}
		}
	}
}
