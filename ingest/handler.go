package ingest

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var module struct {
	batcher *Batcher
}

// Register mounts the events ingest route and starts the flush pipeline.
// It also registers the flag-ID cache eviction hooks so flag writes in
// this process invalidate cached (project,key)->flagID mappings.
func Register(se *core.ServeEvent) {
	module.batcher = NewBatcher(se.App)
	module.batcher.Start()
	RegisterFlagCacheHooks(se.App)
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
	_, env, err := RequireKeyEnv(re, KindIngest)
	if errors.Is(err, ErrKeyResponded) {
		return nil // saturation/storage/rate response already written
	}
	if err != nil {
		return err
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
	flagIDs := resolveFlagIDs(re.App, env.GetString("project"), events)
	stored := make([]StoredEvent, 0, len(events))
	for _, ev := range events {
		ts := ev.Ts
		if ts.IsZero() {
			ts = now
		}
		stored = append(stored, StoredEvent{
			EnvID: env.Id,
			Kind:  ev.Kind, Flag: flagIDs[ev.Flag], Variant: ev.Variant,
			UserHash: ResolveUserHash(ev.UserHash, ""), Ts: ts,
			Version: ev.Version,
		})
	}
	if !module.batcher.EnqueueAll(stored) {
		// Buffer cannot fit the whole batch (see drop-vs-block note in
		// batcher.go): nothing was queued, so a client retry resends
		// exactly this batch with no duplicates.
		return re.JSON(http.StatusServiceUnavailable, map[string]any{"message": "Ingest buffer full, retry.", "status": 503})
	}
	return re.JSON(http.StatusAccepted, map[string]any{"accepted": len(stored), "status": 202})
}

func writeAPIError(re *core.RequestEvent, aerr *apiError) error {
	return re.JSON(aerr.Status, map[string]any{"message": aerr.Msg, "status": aerr.Status})
}

// resolveFlagIDs maps each validated event's verbatim flag key to the
// flags record ID scoped to the event env's project (key + project,
// never global key): the same key under another project never matches.
// Each distinct non-empty key costs at most one indexed lookup (cached hot keys cost zero); empty keys stay
// "" (env-wide) without a lookup. Unknown keys resolve to "" so the
// event is stored with the flag relation unset (kind/variant preserved).
// A missing flags collection or any lookup error degrades the same way
// ("" for every key) — ingest stays available and attribution falls back
// to env-wide instead of failing the batch.
func resolveFlagIDs(app core.App, projectID string, events []EventIn) map[string]string {
	out := make(map[string]string, len(events))
	if projectID == "" {
		return out
	}
	for _, ev := range events {
		if ev.Flag == "" {
			continue
		}
		if _, done := out[ev.Flag]; done {
			continue
		}
		if id, ok := flagCacheGet(projectID, ev.Flag); ok {
			out[ev.Flag] = id
			continue
		}
		flagLookupCalls.Add(1)
		recs, err := app.FindRecordsByFilter("flags", "project = {:p} && key = {:k}", "", 1, 0, dbx.Params{"p": projectID, "k": ev.Flag})
		if err != nil || len(recs) == 0 {
			out[ev.Flag] = ""
			flagCachePut(projectID, ev.Flag, "")
			continue
		}
		out[ev.Flag] = recs[0].Id
		flagCachePut(projectID, ev.Flag, recs[0].Id)
	}
	return out
}
