package stream

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/ingest"
	"github.com/configwire/configwire/security"

	"github.com/pocketbase/pocketbase/core"
)

// maxStreamsPerKey caps concurrent SSE connections per SDK key (keyed by
// key.Id — v2 rows store hash="", so the stored hash cannot key this).
const maxStreamsPerKey = 5

// keepaliveInterval and maxStreamDuration are atomic so tests can
// shorten them without racing a running handler; production values are
// the documented 20s / 10min.
var (
	keepaliveInterval atomic.Int64
	maxStreamDuration atomic.Int64
)

func init() {
	keepaliveInterval.Store(int64(20 * time.Second))
	maxStreamDuration.Store(int64(10 * time.Minute))
}

var (
	streamMu     sync.Mutex
	streamActive = make(map[string]int)
)

// Register mounts the production SSE stream endpoint. It must be called
// BEFORE the catch-all /{path...} static route so /api/* keeps precedence.
func Register(se *core.ServeEvent) {
	se.Router.GET("/api/v1/env/{env}/stream", getStream)
	se.Router.OPTIONS("/api/v1/env/{env}/stream", optionsStream)
}

// streamAllowHeaders mirrors fetch: browser streaming via fetch() sends
// the SDK key as a header, so the preflight must permit it.
const streamAllowHeaders = "X-ConfigWire-Key"

func corsOrigin() string {
	if v := os.Getenv("CONFIGWIRE_CORS_ORIGIN"); v != "" {
		return v
	}
	return "*"
}

func setCORS(re *core.RequestEvent) {
	re.Response.Header().Set("Access-Control-Allow-Origin", corsOrigin())
}

// optionsStream answers the CORS preflight for the stream (no auth:
// preflights carry no credentials by spec).
func optionsStream(re *core.RequestEvent) error {
	security.SetHeaders(re)
	h := re.Response.Header()
	h.Set("Access-Control-Allow-Origin", corsOrigin())
	h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	h.Set("Access-Control-Allow-Headers", streamAllowHeaders)
	h.Set("Access-Control-Max-Age", "86400")
	return re.NoContent(http.StatusNoContent)
}

// RegisterHook fans publish/rollback release creates out to connected
// subscribers. It binds OnRecordAfterCreateSuccess (PocketBase v0.40.4
// core/app.go:1052, impl core/base.go:970), which fires only after a
// successful create — so rejected writes never notify:
//
//   - publish/rollback handler saves (releases.IsInternalSaveCtx) succeed
//     and notify;
//   - direct data-API writes to releases are rejected by the Wave-1
//     publish-only guard in main.go (OnRecordCreate, before any create),
//     so they never reach a successful create and never notify.
func RegisterHook(app core.App) {
	app.OnRecordAfterCreateSuccess("releases").BindFunc(func(e *core.RecordEvent) error {
		rec := e.Record
		Notify(rec.GetString("env"), Update{
			Version: rec.GetInt("version"),
			Etag:    rec.GetString("etag"),
			Env:     rec.GetString("env"),
		})
		return e.Next()
	})
}

// formatUpdate renders one config_update SSE frame: an event line, a JSON
// data line carrying the release version/etag/env id, and the blank-line
// terminator the client buffers on.
func formatUpdate(u Update) string {
	data, err := json.Marshal(map[string]any{
		"version": u.Version,
		"etag":    u.Etag,
		"env":     u.Env,
	})
	if err != nil {
		data = []byte(`{}`)
	}
	return "event: config_update\n" + "data: " + string(data) + "\n\n"
}

// getStream handles GET /api/v1/env/:env/stream.
// Order (same as fetch): 401 (key) -> 404 (unknown slug) / 401 (key-env
// mismatch). No canned event on connect (avoids stale-event churn;
// freshness is covered by the documented poll fallback).
// Lifecycle: the loop runs inline on the request goroutine — no
// per-connection goroutine is spawned, ticker/timer stop via defer, and
// every return path ends the handler, so nothing leaks after close.
func getStream(re *core.RequestEvent) error {
	security.SetHeaders(re)
	setCORS(re)
	key, err := ingest.RequireSDKKey(re)
	if errors.Is(err, ingest.ErrKeyResponded) {
		return nil // saturation/storage response already written
	}
	if err != nil {
		return err
	}
	slug := re.Request.PathValue("env")
	// Deterministic: the key's env IS the env, so a slug shared by
	// several projects can never misroute here.
	env, err := envresolve.ResolveForKey(re.App, slug, key.GetString("env"))
	if err != nil {
		return envresolve.ToRequestError(re, err)
	}
	streamMu.Lock()
	if streamActive[key.Id] >= maxStreamsPerKey {
		streamMu.Unlock()
		return re.JSON(http.StatusTooManyRequests, map[string]any{"message": "Too many concurrent streams.", "status": 429})
	}
	streamActive[key.Id]++
	streamMu.Unlock()
	defer func() {
		streamMu.Lock()
		streamActive[key.Id]--
		if streamActive[key.Id] <= 0 {
			delete(streamActive, key.Id)
		}
		streamMu.Unlock()
	}()
	ch, unsub := subscribe(env.Id)
	defer unsub()
	h := re.Response.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Vary", "X-ConfigWire-Key")
	h.Set("X-Accel-Buffering", "no")
	if err := re.Flush(); err != nil {
		// Client gone before headers committed: a disconnect, not a
		// server error. Returning err here would log a 500 for it.
		return nil
	}
	ticker := time.NewTicker(time.Duration(keepaliveInterval.Load()))
	defer ticker.Stop()
	deadline := time.NewTimer(time.Duration(maxStreamDuration.Load()))
	defer deadline.Stop()
	ctx := re.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-ticker.C:
			if _, err := re.Response.Write([]byte(": ping\n\n")); err != nil {
				return nil
			}
			if err := re.Flush(); err != nil {
				return nil
			}
		case u := <-ch:
			if _, err := re.Response.Write([]byte(formatUpdate(u))); err != nil {
				return nil
			}
			if err := re.Flush(); err != nil {
				return nil
			}
		}
	}
}
