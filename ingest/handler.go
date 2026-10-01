package ingest

import (
	"log"
	"net/http"
	"time"

	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/limits"

	"github.com/pocketbase/pocketbase/core"
)

var module struct {
	batcher *Batcher
}

// Register mounts the events ingest route and starts the flush pipeline.
func Register(se *core.ServeEvent) {
	module.batcher = NewBatcher(se.App)
	module.batcher.Start()
	se.Router.POST("/api/v1/env/{env}/events", postEvents)
	se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		module.batcher.Stop()
		return e.Next()
	})
}

// EnableWAL switches SQLite to WAL mode on serve. The store is not yet open
// during OnBootstrap, so this binds OnServe.
// The mode is persistent in the DB file; verify with `sqlite3 <dir>/data.db "pragma journal_mode;"` → wal.
func EnableWAL(app core.App) {
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if _, err := e.App.DB().NewQuery("PRAGMA journal_mode=WAL").Execute(); err != nil {
			e.App.Logger().Warn("ingest: failed to enable WAL", "error", err)
		} else {
			log.Print("ingest: SQLite journal_mode set to WAL")
		}
		return e.Next()
	})
}

// Order: 401 (key) → 404 (env) / 400 (ambiguous slug) / 401 (scope) →
// 429 (rate) → 400/413 (body) → 202.
// Uses re.App for every request-scoped lookup (never a captured app).
func postEvents(re *core.RequestEvent) error {
	key, err := RequireSDKKey(re)
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
	if !limits.AllowIngestKey(key.Id, limits.EffectiveIngestRps(key)) {
		re.Response.Header().Set("Retry-After", "1")
		return re.JSON(http.StatusTooManyRequests, map[string]any{"message": "Rate limit exceeded.", "status": 429})
	}
	body, aerr := readBody(re)
	if aerr != nil {
		return writeAPIError(re, aerr)
	}
	events, aerr := ValidateBody(body)
	if aerr != nil {
		return writeAPIError(re, aerr)
	}
	now := time.Now().UTC()
	stored := make([]StoredEvent, 0, len(events))
	for _, ev := range events {
		ts := ev.Ts
		if ts.IsZero() {
			ts = now
		}
		stored = append(stored, StoredEvent{
			EnvID: env.Id,
			Kind:  ev.Kind, Variant: ev.Variant,
			UserHash: ResolveUserHash(ev.UserHash, ""), Ts: ts,
			Version: ev.Version,
		})
	}
	for _, s := range stored {
		if !module.batcher.Enqueue(s) {
			// Buffer saturated (see drop-vs-block note in batcher.go).
			return re.JSON(http.StatusServiceUnavailable, map[string]any{"message": "Ingest buffer full, retry.", "status": 503})
		}
	}
	return re.JSON(http.StatusAccepted, map[string]any{"accepted": len(stored), "status": 202})
}

func writeAPIError(re *core.RequestEvent, aerr *apiError) error {
	return re.JSON(aerr.Status, map[string]any{"message": aerr.Msg, "status": aerr.Status})
}
