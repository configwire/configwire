package stream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/configwire/configwire/ingest"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
)

// streamTestApp boots a TestApp with the three collections getStream and
// the fan-out hook read: environments, sdk_keys (plain-text env holding
// the env id, exactly like the handler path), and releases.
func streamTestApp(t *testing.T) *tests.TestApp {
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
	resetHub(t)
	return app
}

func seedEnv(t *testing.T, app *tests.TestApp, slug string) string {
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

// seedLegacyKey stores a v1-style row (prefix + fast hash, no verifier)
// so tests exercise the constant-time hash path without bcrypt cost.
func seedLegacyKey(t *testing.T, app *tests.TestApp, full, envID string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("prefix", ingest.KeyPrefix(full))
	rec.Set("hash", ingest.KeyHash(full))
	rec.Set("revoked", false)
	rec.Set("env", envID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save sdk key: %v", err)
	}
	return rec.Id
}

// callStream invokes getStream directly and reports the HTTP status,
// unwrapping router ApiErrors for non-2xx paths (same pattern as the
// account package tests). Callers needing the success path use
// callStreamAsync: getStream blocks until the request context ends.
func callStream(app core.App, ctx context.Context, slug, key string) (int, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/env/"+slug+"/stream", nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	req.SetPathValue("env", slug)
	if key != "" {
		req.Header.Set(ingest.HeaderKey, key)
	}
	req.Header.Set("Accept", "text/event-stream")
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getStream(re); err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status, rec
		}
		return -1, rec
	}
	return rec.Code, rec
}

func TestStreamAuthOrder(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	seedEnv(t, app, "other")
	const goodKey = "cw-stream-good-0001"
	seedLegacyKey(t, app, goodKey, envA)

	if code, _ := callStream(app, nil, "dev", ""); code != http.StatusUnauthorized {
		t.Errorf("no key: status = %d, want 401", code)
	}
	if code, _ := callStream(app, nil, "dev", "cw-unknown-xxxx"); code != http.StatusUnauthorized {
		t.Errorf("unknown key: status = %d, want 401", code)
	}
	// The key is bound to a missing env row: ResolveForKey cannot find
	// the env, so the slug is unknown -> 404.
	seedLegacyKey(t, app, "cw-stream-dangling-02", "missing-env-id")
	if code, _ := callStream(app, nil, "nope", "cw-stream-dangling-02"); code != http.StatusNotFound {
		t.Errorf("unknown env: status = %d, want 404", code)
	}
	// Key bound to env "dev" requesting env "other" -> scope 401.
	if code, _ := callStream(app, nil, "other", goodKey); code != http.StatusUnauthorized {
		t.Errorf("env mismatch: status = %d, want 401", code)
	}
}

// TestStreamCORSAcrossResponses pins fetch parity: stream responses
// carry Access-Control-Allow-Origin on both the 401 and 200 paths,
// default "*" and honoring CONFIGWIRE_CORS_ORIGIN like fetch.
func TestStreamCORSAcrossResponses(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	const goodKey = "cw-stream-cors-0001"
	seedLegacyKey(t, app, goodKey, envA)

	if code, rec := callStream(app, nil, "dev", ""); code != http.StatusUnauthorized {
		t.Fatalf("no key: status = %d, want 401", code)
	} else if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("401 ACAO = %q, want dev default *", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, rec := callStream(app, ctx, "dev", goodKey)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("200 ACAO = %q, want dev default *", got)
	}

	t.Setenv("CONFIGWIRE_CORS_ORIGIN", "https://app.example.com")
	_, rec = callStream(app, nil, "dev", "")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("configured ACAO = %q, want the configured origin", got)
	}
}

// TestStreamAllowHeadersIsKey pins the preflight contract: the stream
// OPTIONS response must permit the SDK key header.
func TestStreamAllowHeadersIsKey(t *testing.T) {
	if streamAllowHeaders != "X-ConfigWire-Key" {
		t.Fatalf("streamAllowHeaders = %q, want %q", streamAllowHeaders, "X-ConfigWire-Key")
	}
}

func TestStreamValidHeadersAndCleanup(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	const goodKey = "cw-stream-valid-0001"
	keyID := seedLegacyKey(t, app, goodKey, envA)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var rec *httptest.ResponseRecorder
	go func() {
		code, r := callStream(app, ctx, "dev", goodKey)
		rec = r
		done <- code
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("valid stream: status = %d, want 200", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("valid stream: handler did not return after context cancel")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if v := rec.Header().Get("Vary"); v != "X-ConfigWire-Key" {
		t.Errorf("Vary = %q, want X-ConfigWire-Key", v)
	}
	if xb := rec.Header().Get("X-Accel-Buffering"); xb != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", xb)
	}
	streamMu.Lock()
	_, stillTracked := streamActive[keyID]
	streamMu.Unlock()
	if stillTracked {
		t.Error("connection must be untracked after the handler returns")
	}
}

func TestStreamOverCapIs429(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	const goodKey = "cw-stream-cap-0001"
	keyID := seedLegacyKey(t, app, goodKey, envA)

	streamMu.Lock()
	streamActive[keyID] = maxStreamsPerKey
	streamMu.Unlock()
	t.Cleanup(func() {
		streamMu.Lock()
		delete(streamActive, keyID)
		streamMu.Unlock()
	})

	code, rec := callStream(app, nil, "dev", goodKey)
	if code != http.StatusTooManyRequests {
		t.Fatalf("over-cap: status = %d, want 429", code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("over-cap body is not JSON: %v", err)
	}
	if body["message"] != "Too many concurrent streams." || body["status"] != float64(429) {
		t.Fatalf("over-cap body = %v, want message/status 429 shape", body)
	}
}

// TestFormatUpdateFrame pins the wire bytes the Dart client parses: an
// "event: config_update" line, a JSON data line with version/etag/env,
// and the blank-line terminator.
func TestFormatUpdateFrame(t *testing.T) {
	frame := formatUpdate(Update{Version: 3, Etag: "etag-3", Env: "envX"})
	if !strings.HasPrefix(frame, "event: config_update\n") {
		t.Fatalf("frame must start with the event line, got %q", frame)
	}
	if !strings.HasSuffix(frame, "\n\n") {
		t.Fatalf("frame must end with a blank line, got %q", frame)
	}
	lines := strings.Split(strings.TrimSuffix(frame, "\n\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "data: ") {
		t.Fatalf("frame must be exactly event + data lines, got %q", frame)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &data); err != nil {
		t.Fatalf("data line is not JSON: %v", err)
	}
	if data["version"] != float64(3) || data["etag"] != "etag-3" || data["env"] != "envX" {
		t.Fatalf("data = %v, want version 3 etag-3 envX", data)
	}
}

// saveReleaseRow inserts one releases row through the hooked app (the
// publish/rollback internal-save path shape: a successful create).
func saveReleaseRow(t *testing.T, app *tests.TestApp, envID string, version int, etag string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", envID)
	rec.Set("version", version)
	rec.Set("etag", etag)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save release v%d: %v", version, err)
	}
}

// TestHookFanoutOnReleaseCreate pins the push contract: a release create
// notifies the subscriber of that env with the row's version/etag/env,
// while another env's subscriber hears nothing.
func TestHookFanoutOnReleaseCreate(t *testing.T) {
	app := streamTestApp(t)
	RegisterHook(app)
	const envA, envB = "env-id-a", "env-id-b"
	chA, unsubA := subscribe(envA)
	defer unsubA()
	chB, unsubB := subscribe(envB)
	defer unsubB()

	saveReleaseRow(t, app, envA, 2, "etag-v2")

	select {
	case u := <-chA:
		if u.Version != 2 || u.Etag != "etag-v2" || u.Env != envA {
			t.Fatalf("fan-out update = %+v, want version 2 etag-v2 envA", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber received no config_update after release create")
	}
	select {
	case u := <-chB:
		t.Fatalf("other-env subscriber must hear nothing, got %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHookDirectWriteRejectedNeverNotifies verifies the Wave-1 reasoning:
// the publish-only guard rejects direct data-API writes at OnRecordCreate,
// so they never reach a successful create and the AfterCreateSuccess hook
// never fires for them.
func TestHookDirectWriteRejectedNeverNotifies(t *testing.T) {
	app := streamTestApp(t)
	RegisterHook(app)
	publishOnly := apis.NewBadRequestError("releases are publish-only: use publish/rollback endpoints", nil)
	app.OnRecordCreate("releases").BindFunc(func(e *core.RecordEvent) error {
		return publishOnly
	})

	ch, unsub := subscribe("envA")
	defer unsub()

	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", "envA")
	rec.Set("version", 1)
	rec.Set("etag", "etag-1")
	if err := app.Save(rec); err == nil {
		t.Fatal("direct releases write must be rejected by the publish-only guard")
	}
	select {
	case u := <-ch:
		t.Fatalf("rejected write must not notify, got %+v", u)
	case <-time.After(100 * time.Millisecond):
	}
}
