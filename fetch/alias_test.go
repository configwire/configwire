package fetch

// Alias-route tests for the key-derived fetch endpoint plus legacy parity.
//
// GET /api/v1/config serves getConfig with no {env} segment, so the env
// comes purely from the SDK key. The legacy /api/v1/env/{env}/config
// route serves the same handler with slug validation. A client-sent ?env=
// query value of any shape is accepted-but-ignored (never 400/401/404
// from it).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"github.com/configwire/configwire/ingest"
	"github.com/configwire/configwire/internal/testhelp"
)

func fetchAliasTestApp(t *testing.T) *tests.TestApp {
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
		`{"type":"text","name":"revoked"}`,
		`{"type":"text","name":"env"}`,
	} {
		if err := keys.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add sdk_keys field %s: %v", f, err)
		}
	}
	if err := app.Save(keys); err != nil {
		t.Fatalf("save sdk_keys collection: %v", err)
	}

	rels := core.NewBaseCollection("releases")
	for _, f := range []string{
		`{"type":"text","name":"env"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"text","name":"etag"}`,
	} {
		if err := rels.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add releases field %s: %v", f, err)
		}
	}
	if err := app.Save(rels); err != nil {
		t.Fatalf("save releases collection: %v", err)
	}

	ingest.ResetKeyCache()
	t.Cleanup(ingest.ResetKeyCache)
	return app
}

func seedFetchAliasEnv(t *testing.T, app *tests.TestApp, slug string) string {
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

func seedFetchAliasKey(t *testing.T, app *tests.TestApp, full, envID string) {
	t.Helper()
	verifier, err := ingest.GenerateVerifier(full)
	if err != nil {
		t.Fatalf("GenerateVerifier: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("prefix", ingest.KeyPrefix(full))
	rec.Set("hash", "")
	rec.Set("keyVer", 2)
	rec.Set("verifier", verifier)
	rec.Set("revoked", false)
	rec.Set("env", envID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save sdk key: %v", err)
	}
}

// callFetchAlias invokes getConfig on the key-only route
// (/api/v1/config, never a path slug). Extra query (e.g. ?env=) rides
// on path.
func callFetchAlias(app core.App, path, key string) (int, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set(ingest.HeaderKey, key)
	}
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getConfig(re); err != nil {
		return testhelp.Code(err, rec), rec
	}
	return rec.Code, rec
}

func decodeFetchBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("fetch body is not JSON: %v", err)
	}
	return body
}

// TestFetchAliasEmptyRelease pins the key-only empty-release 200
// (version 0, etag "none", empty values) on the bare path.
func TestFetchAliasEmptyRelease(t *testing.T) {
	app := fetchAliasTestApp(t)
	envDev := seedFetchAliasEnv(t, app, "dev")
	const goodKey = "cw-fetch-alias-0001"
	seedFetchAliasKey(t, app, goodKey, envDev)

	aliasCode, aliasRec := callFetchAlias(app, "/api/v1/config", goodKey)
	if aliasCode != http.StatusOK {
		t.Fatalf("alias fetch: status = %d, want 200", aliasCode)
	}
	aliasBody := decodeFetchBody(t, aliasRec)
	if aliasBody["version"] != float64(0) || aliasBody["etag"] != "none" {
		t.Errorf("alias empty-release body = %v, want version 0 etag none", aliasBody)
	}
	if got := aliasRec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("alias 200 ACAO = %q, want dev default *", got)
	}
}

// TestFetchAliasIgnoresEnvQuery pins that ?env= of any shape (valid
// slug, wrong slug, garbage, blank) is accepted-but-ignored: the fetch
// succeeds on the key alone, and a trimmed non-empty value stamps the
// deprecated-env header.
func TestFetchAliasIgnoresEnvQuery(t *testing.T) {
	app := fetchAliasTestApp(t)
	envDev := seedFetchAliasEnv(t, app, "dev")
	seedFetchAliasEnv(t, app, "other")
	const goodKey = "cw-fetch-alias-envq-0002"
	seedFetchAliasKey(t, app, goodKey, envDev)

	for _, q := range []string{"dev", "other", "no-such-env", "  garbage  ", "", "   "} {
		code, rec := callFetchAlias(app, "/api/v1/config?env="+url.QueryEscape(q), goodKey)
		if code != http.StatusOK {
			t.Errorf("?env=%q: status = %d, want 200", q, code)
			continue
		}
		body := decodeFetchBody(t, rec)
		if body["version"] != float64(0) || body["etag"] != "none" {
			t.Errorf("?env=%q: body = %v, want empty-release 200", q, body)
		}
		trimmed := q != "" && len(trim(q)) > 0
		if got := rec.Header().Get("X-CW-Deprecated-env"); trimmed && got != "true" {
			t.Errorf("?env=%q: deprecated-env header = %q, want true", q, got)
		} else if !trimmed && got != "" {
			t.Errorf("?env=%q: deprecated-env header = %q, want absent", q, got)
		}
	}
}

func trim(s string) string {
	return strings.TrimSpace(s)
}

func TestFetchAliasAuthOrder(t *testing.T) {
	app := fetchAliasTestApp(t)
	envDev := seedFetchAliasEnv(t, app, "dev")
	seedFetchAliasEnv(t, app, "other")
	const goodKey = "cw-fetch-alias-0002"
	seedFetchAliasKey(t, app, goodKey, envDev)

	if code, _ := callFetchAlias(app, "/api/v1/config", ""); code != http.StatusUnauthorized {
		t.Errorf("alias no key: status = %d, want 401", code)
	}
	if code, _ := callFetchAlias(app, "/api/v1/config", "cw-unknown-xxxx"); code != http.StatusUnauthorized {
		t.Errorf("alias unknown key: status = %d, want 401", code)
	}
	seedFetchAliasKey(t, app, "cw-fetch-alias-dangling-03", "missing-env-id")
	rec404 := httptest.NewRecorder()
	req404 := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req404.Header.Set(ingest.HeaderKey, "cw-fetch-alias-dangling-03")
	re404 := &core.RequestEvent{App: app}
	re404.Request = req404
	re404.Response = rec404
	if code, msg := testhelp.CodeMessage(getConfig(re404), rec404); code != http.StatusNotFound {
		t.Errorf("alias dangling env: status = %d, want 404", code)
	} else if msg != "Unknown env." {
		t.Errorf("alias dangling env message = %q, want Unknown env.", msg)
	}
	// Key-only: the same key succeeds with no slug and ignores any ?env=.
	if code, _ := callFetchAlias(app, "/api/v1/config?env=other", goodKey); code != http.StatusOK {
		t.Errorf("alias ?env=other ignored: status = %d, want 200", code)
	}
}

func callFetchLegacy(app core.App, slug, key string) (int, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/env/"+slug+"/config", nil)
	req.SetPathValue("env", slug)
	if key != "" {
		req.Header.Set(ingest.HeaderKey, key)
	}
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getConfig(re); err != nil {
		return testhelp.Code(err, rec), rec
	}
	return rec.Code, rec
}

func TestFetchLegacyAliasParity(t *testing.T) {
	app := fetchAliasTestApp(t)
	envDev := seedFetchAliasEnv(t, app, "dev")
	seedFetchAliasEnv(t, app, "other")
	const goodKey = "cw-fetch-alias-0002"
	seedFetchAliasKey(t, app, goodKey, envDev)

	legacyCode, legacyRec := callFetchLegacy(app, "dev", goodKey)
	if legacyCode != http.StatusOK {
		t.Fatalf("legacy fetch: status = %d, want 200", legacyCode)
	}
	aliasCode, aliasRec := callFetchAlias(app, "/api/v1/config", goodKey)
	if aliasCode != http.StatusOK {
		t.Fatalf("alias fetch: status = %d, want 200", aliasCode)
	}
	legacyBody := decodeFetchBody(t, legacyRec)
	aliasBody := decodeFetchBody(t, aliasRec)
	if legacyBody["version"] != aliasBody["version"] || legacyBody["etag"] != aliasBody["etag"] {
		t.Errorf("legacy vs alias bodies differ: %v vs %v", legacyBody, aliasBody)
	}
	if code, _ := callFetchLegacy(app, "other", goodKey); code != http.StatusUnauthorized {
		t.Errorf("legacy wrong-env: status = %d, want 401", code)
	}
}

// TestFetchAliasEnvStampedOnBadBucket pins that the ?env= compat header
// is stamped before bucket validation: a malformed ?b= still answers 400
// with X-CW-Deprecated-env: true when ?env= is sent (absent otherwise).
func TestFetchAliasEnvStampedOnBadBucket(t *testing.T) {
	app := fetchAliasTestApp(t)
	envDev := seedFetchAliasEnv(t, app, "dev")
	const goodKey = "cw-fetch-alias-envhdr-01"
	seedFetchAliasKey(t, app, goodKey, envDev)

	code, rec := callFetchAlias(app, "/api/v1/config?env=dev&b=bogus", goodKey)
	if code != http.StatusBadRequest {
		t.Fatalf("bad ?b= status = %d, want 400", code)
	}
	if got := rec.Header().Get("X-CW-Deprecated-env"); got != "true" {
		t.Errorf("bad ?b= deprecated-env header = %q, want true", got)
	}

	code, rec = callFetchAlias(app, "/api/v1/config?b=bogus", goodKey)
	if code != http.StatusBadRequest {
		t.Fatalf("bad ?b= (no env) status = %d, want 400", code)
	}
	if got := rec.Header().Get("X-CW-Deprecated-env"); got != "" {
		t.Errorf("bad ?b= (no env) deprecated-env header = %q, want absent", got)
	}
}

// TestFetchAliasOptionsParity pins the alias preflight: same 204 shape.
func TestFetchAliasOptionsParity(t *testing.T) {
	app := fetchAliasTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/config", nil)
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := optionsConfig(re); err != nil {
		t.Fatalf("alias OPTIONS err = %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("alias OPTIONS status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
		t.Errorf("alias OPTIONS Allow-Methods = %q, want GET, OPTIONS", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != allowHeaders {
		t.Errorf("alias OPTIONS Allow-Headers = %q, want %q", got, allowHeaders)
	}
}
