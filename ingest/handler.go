package ingest

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/identity"
	"github.com/configwire/configwire/limits"
	"github.com/configwire/configwire/logstore"
	"github.com/configwire/configwire/logx"
)

var module struct {
	batcher *Batcher
}

// uidCompatHeader signals that a client-sent ?uid= was
// accepted-but-ignored: attribution always uses deriveIdentity (valid ?b=
// else empty). Spelling mirrors v0.1.9.
const uidCompatHeader = "X-CW-Deprecated-uid"

// envCompatHeader signals that a client-sent ?env= was
// accepted-but-ignored: env resolves purely from the SDK key.
const envCompatHeader = "X-CW-Deprecated-env"

// stampUidCompat sets the compat header when the client sent a trimmed
// non-empty ?uid= (accepted-but-ignored). Absent/empty/blank sends no
// header.
func stampUidCompat(re *core.RequestEvent) {
	if strings.TrimSpace(re.Request.URL.Query().Get("uid")) != "" {
		re.Response.Header().Set(uidCompatHeader, "true")
	}
}

// stampEnvCompat sets the compat header when the client sent a trimmed
// non-empty ?env= (accepted-but-ignored). Absent/empty/blank sends no
// header. The value is never logged.
func stampEnvCompat(re *core.RequestEvent) {
	if strings.TrimSpace(re.Request.URL.Query().Get("env")) != "" {
		re.Response.Header().Set(envCompatHeader, "true")
	}
}

// deriveIdentity resolves (bucket, country) for one ingest request.
// Identity is the stable ?b= bucket only: valid ?b= wins verbatim, else ""
// (unattributed, no anonID fallback); client ?userHash= and ?uid= (any
// shape) are accepted-but-ignored and never become the bucket; postEvents
// stamps X-CW-Deprecated-uid when a trimmed non-empty ?uid= is sent;
// country = first valid ISO-2 in the country-header order ("" when absent).
// A malformed ?b= is a 400 (returned as *apiError, mirroring the fetch
// ?b= 400) — never a silent fallback, so fetch arms and exposure
// attribution stay joined per install.
//
// Canonical ?b= rule lives in identity.ParseBucketParam — fetch
// imports ingest (RequireKeyEnv), so ingest cannot import fetch back
// (import cycle); both delegate to identity (stdlib-only leaf).
// parseBucketCanonical below wraps the identity error in badRequest so
// the 400 text stays byte-identical.
func deriveIdentity(re *core.RequestEvent) (string, string, *apiError) {
	country := identity.CountryFromHeadersWith(re.Request.Header, limits.GetConfig().CountryHeaders)
	q := re.Request.URL.Query()
	if raw := q.Get("b"); raw != "" {
		b, err := parseBucketCanonical(raw)
		if err != nil {
			return "", "", err
		}
		return b, country, nil
	}
	return "", country, nil
}

// parseBucketCanonical validates the stable-bucket ?b= query param.
// Thin delegate over identity.ParseBucketParam (canonical; see above why
// fetch and ingest share the rule via identity instead of duplicating
// it): plain int string "0"-"9999", no coercion. Wraps the identity
// error in badRequest so the 400 text stays byte-identical.
func parseBucketCanonical(raw string) (string, *apiError) {
	b, err := identity.ParseBucketParam(raw)
	if err != nil {
		return "", badRequest(err.Error())
	}
	return b, nil
}

// acceptWrite dual-writes one success line: stderr via reqLog and a
// persisted server_logs row (mirrors denyWrite's shape, Info level for
// success). reason is a decomposed code (accepted — never free text);
// status is the client-facing status. keyPrefix carries KeyPrefix(full)
// and is appended as key_prefix=<prefix> only when non-empty (a key was
// presented — never the full key). projectID is the env's project record
// ID, appended as project=<id> (IDs never contain spaces, so the k=v
// line stays parseable) so the log viewer can show the project name;
// empty stays omitted. Success lines skip the store (but keep the stderr
// line) when the successLogs setting is off; rejections always persist.
// The message stays a single
// space-separated k=v line. Never pass full keys, tokens, emails, IPs,
// buckets, or attrs. reqLog.Request keeps the same reason string.
// Write errors are best-effort (swallowed, never retry-block the request).
func acceptWrite(re *core.RequestEvent, reason string, status int, keyPrefix, projectID string) {
	reqLog.Request(re.Request.Method, re.Request.URL.Path, reason, status)
	if !logstore.GetConfig().SuccessLogs {
		return
	}
	msg := fmt.Sprintf("%s %s reason=%s status=%d", re.Request.Method, re.Request.URL.Path, reason, status)
	if keyPrefix != "" {
		msg += " key_prefix=" + keyPrefix
	}
	if projectID != "" {
		msg += " project=" + projectID
	}
	_ = logstore.Write(re.App, int(logx.LevelInfo), "ingest", msg)
}

// Register mounts the events ingest route and starts the flush pipeline.
// It also registers the flag-ID cache eviction hooks so flag writes in
// this process invalidate cached (project,key)->flagID mappings.
func Register(se *core.ServeEvent) {
	module.batcher = NewBatcher(se.App)
	module.batcher.Start()
	RegisterFlagCacheHooks(se.App)
	se.Router.POST("/api/v1/env/{env}/events", postEvents)
	se.Router.POST("/api/v1/events", postEvents)
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
// 429 (rate) → 400/413 (body) → 202. A client ?env= query value of any
// shape is accepted-but-ignored (never 400/401/404 from it).
// Uses re.App for every request-scoped lookup (never a captured app).
func postEvents(re *core.RequestEvent) error {
	_, env, err := RequireKeyEnv(re, KindIngest)
	if errors.Is(err, ErrKeyResponded) {
		return nil // saturation/storage/rate response already written
	}
	if err != nil {
		return err
	}
	// The ?env= compat stamp runs before body/bucket validation so
	// malformed bodies and ?b= still carry the header on their 400/413
	// (the stamp only reads the query, never the validated values).
	stampEnvCompat(re)
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
	bucket, country, derr := deriveIdentity(re)
	if derr != nil {
		return writeAPIError(re, derr)
	}
	stampUidCompat(re)
	stored := make([]StoredEvent, 0, len(events))
	for _, ev := range events {
		ts := ev.Ts
		if ts.IsZero() {
			ts = now
		}
		// Bucket-only storage: a client ?userHash= query value of any shape
		// is accepted-but-ignored; the stable ?b= bucket wins verbatim when
		// present, else "" (unattributed), so fetch bucketing and exposures
		// join on the same value. Per-event body userHash stays
		// accepted-but-ignored and never reaches storage.
		stored = append(stored, StoredEvent{
			EnvID: env.Id,
			Kind:  ev.Kind, Flag: flagIDs[ev.Flag], Variant: ev.Variant,
			Bucket: bucket, Country: country, Ts: ts,
			Version: ev.Version,
		})
	}
	if !module.batcher.EnqueueAll(stored) {
		// Buffer cannot fit the whole batch (see drop-vs-block note in
		// batcher.go): nothing was queued, so a client retry resends
		// exactly this batch with no duplicates.
		return re.JSON(http.StatusServiceUnavailable, map[string]any{"message": "Ingest buffer full, retry.", "status": 503})
	}
	acceptWrite(re, "accepted", http.StatusAccepted, KeyPrefix(re.Request.Header.Get(HeaderKey)), env.GetString("project"))
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
