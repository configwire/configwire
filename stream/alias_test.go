package stream

// Alias-route tests for the key-derived stream endpoint plus legacy parity.
//
// GET /api/v1/stream serves getStream with no {env} segment, so the env
// comes purely from the SDK key. The legacy /api/v1/env/{env}/stream
// route serves the same handler with slug validation. A client-sent ?env=
// query value of any shape is accepted-but-ignored.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/configwire/configwire/ingest"
	"github.com/configwire/configwire/internal/testhelp"
	"github.com/pocketbase/pocketbase/core"
)

// callStreamAlias invokes getStream on the key-only route
// (/api/v1/stream): no PathValue is set, exactly as the router leaves
// it for a segment-less pattern. Extra query (e.g. ?env=) rides on path.
func callStreamAlias(app core.App, ctx context.Context, path, key string) (int, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if key != "" {
		req.Header.Set(ingest.HeaderKey, key)
	}
	req.Header.Set("Accept", "text/event-stream")
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getStream(re); err != nil {
		return testhelp.Code(err, rec), rec
	}
	return rec.Code, rec
}

func TestStreamAliasAuthOrder(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	seedEnv(t, app, "other")
	const goodKey = "cw-stream-alias-0001"
	seedV2Key(t, app, goodKey, envA)

	if code, _ := callStreamAlias(app, nil, "/api/v1/stream", ""); code != http.StatusUnauthorized {
		t.Errorf("alias no key: status = %d, want 401", code)
	}
	if code, _ := callStreamAlias(app, nil, "/api/v1/stream", "cw-unknown-xxxx"); code != http.StatusUnauthorized {
		t.Errorf("alias unknown key: status = %d, want 401", code)
	}
	seedV2Key(t, app, "cw-stream-alias-dangling-02", "missing-env-id")
	if code, _ := callStreamAlias(app, nil, "/api/v1/stream", "cw-stream-alias-dangling-02"); code != http.StatusNotFound {
		t.Errorf("alias dangling env: status = %d, want 404", code)
	}
}

// TestStreamAliasIgnoresEnvQuery pins that ?env= of any shape is
// accepted-but-ignored on the stream auth path: a wrong slug or garbage
// never 401s the key. The stream blocks on success, so the check runs
// through a cancellable context and asserts the handler reaches the
// 200 streaming state (not an auth error).
func TestStreamAliasIgnoresEnvQuery(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	seedEnv(t, app, "other")
	const goodKey = "cw-stream-alias-envq-0003"
	seedV2Key(t, app, goodKey, envA)

	for _, q := range []string{"dev", "other", "no-such-env", "garbage"} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		go func() {
			code, _ := callStreamAlias(app, ctx, "/api/v1/stream?env="+url.QueryEscape(q), goodKey)
			done <- code
		}()
		time.Sleep(150 * time.Millisecond)
		cancel()
		select {
		case code := <-done:
			if code != http.StatusOK {
				t.Errorf("?env=%q: status = %d, want 200 (streaming)", q, code)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("?env=%q: handler did not return after context cancel", q)
			cancel()
		}
	}
}

// TestStreamAliasValidHeaders pins handler parity: the key-only route
// serves the same 200 SSE headers and untracks the connection on close.
func TestStreamAliasValidHeaders(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	const goodKey = "cw-stream-alias-valid-0001"
	keyID := seedV2Key(t, app, goodKey, envA)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var rec *httptest.ResponseRecorder
	go func() {
		code, r := callStreamAlias(app, ctx, "/api/v1/stream", goodKey)
		rec = r
		done <- code
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("alias valid stream: status = %d, want 200", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("alias valid stream: handler did not return after context cancel")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("alias Content-Type = %q, want text/event-stream", ct)
	}
	if v := rec.Header().Get("Vary"); v != "X-ConfigWire-Key" {
		t.Errorf("alias Vary = %q, want X-ConfigWire-Key", v)
	}
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("alias ACAO = %q, want dev default *", origin)
	}
	streamMu.Lock()
	_, stillTracked := streamActive[keyID]
	streamMu.Unlock()
	if stillTracked {
		t.Error("alias connection must be untracked after the handler returns")
	}
}

// TestStreamAliasOptionsParity pins the alias preflight: same 204 shape.
func TestStreamAliasOptionsParity(t *testing.T) {
	app := streamTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/stream", nil)
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := optionsStream(re); err != nil {
		t.Fatalf("alias OPTIONS err = %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("alias OPTIONS status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
		t.Errorf("alias OPTIONS Allow-Methods = %q, want GET, OPTIONS", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != streamAllowHeaders {
		t.Errorf("alias OPTIONS Allow-Headers = %q, want %q", got, streamAllowHeaders)
	}
}

func TestStreamLegacyWrongEnvIs401(t *testing.T) {
	app := streamTestApp(t)
	envA := seedEnv(t, app, "dev")
	seedEnv(t, app, "other")
	const goodKey = "cw-stream-alias-legacy-0004"
	seedV2Key(t, app, goodKey, envA)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/env/other/stream", nil)
	req.SetPathValue("env", "other")
	req.Header.Set(ingest.HeaderKey, goodKey)
	req.Header.Set("Accept", "text/event-stream")
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if code := testhelp.Code(getStream(re), rec); code != http.StatusUnauthorized {
		t.Errorf("legacy wrong-env: status = %d, want 401", code)
	}
}
