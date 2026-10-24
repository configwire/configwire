package ingest

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/configwire/configwire/identity"
	"github.com/configwire/configwire/limits"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var module struct {
	batcher *Batcher
}

// idManager derives server-side anonIDs (identity.Manager over ClientIP +
// User-Agent + daily salt). MemoryStore is the local default until the
// identity_salts collection is wired as the durable Store; SetIdentityManager
// swaps it in tests.
var idManager = identity.NewManager(identity.NewMemoryStore())

// SetIdentityManager swaps the anonID manager (tests only).
func SetIdentityManager(m *identity.Manager) {
	if m != nil {
		idManager = m
	}
}

// uidCompatHeader signals that a client-sent ?uid= was
// accepted-but-ignored: attribution always uses deriveIdentity (valid ?b=
// else the daily anonID). Spelling mirrors v0.1.9.
const uidCompatHeader = "X-CW-Deprecated-uid"

// stampUidCompat sets the compat header when the client sent a trimmed
// non-empty ?uid= (accepted-but-ignored). Absent/empty/blank sends no
// header.
func stampUidCompat(re *core.RequestEvent) {
	if strings.TrimSpace(re.Request.URL.Query().Get("uid")) != "" {
		re.Response.Header().Set(uidCompatHeader, "true")
	}
}

// deriveIdentity resolves (anonID, country) for one ingest request.
// Identity follows the identical rule as fetch: valid ?b= wins, else the
// daily manager anonID ("" on derivation error — the event still stores,
// unattributed); client ?userHash= and ?uid= (any shape) are
// accepted-but-ignored and never become the anonID; postEvents stamps
// X-CW-Deprecated-uid when a trimmed non-empty ?uid= is sent; country =
// first valid ISO-2 in the country-header order ("" when absent).
// A malformed ?b= is a 400 (returned as *apiError, mirroring the fetch
// ?b= 400) — never a silent fallback, so fetch arms and exposure
// attribution stay joined per install.
//
// Canonical ?b= rule lives in identity.ParseBucketParam — fetch
// imports ingest (RequireKeyEnv), so ingest cannot import fetch back
// (import cycle); both delegate to identity (stdlib-only leaf).
// parseBucketCanonical below wraps the identity error in badRequest so
// the 400 text stays byte-identical.
func deriveIdentity(re *core.RequestEvent, envID string, now time.Time) (string, string, *apiError) {
	country := identity.CountryFromHeadersWith(re.Request.Header, limits.GetConfig().CountryHeaders)
	q := re.Request.URL.Query()
	if raw := q.Get("b"); raw != "" {
		b, err := parseBucketCanonical(raw)
		if err != nil {
			return "", "", err
		}
		return b, country, nil
	}
	anonID, err := idManager.AnonID(envID, limits.ClientIP(re), re.Request.UserAgent(), now)
	if err != nil {
		log.Printf("ingest: anonID derivation failed, storing unattributed: %v", err)
		return "", country, nil
	}
	return anonID, country, nil
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

// Register mounts the events ingest route and starts the flush pipeline.
// It also registers the flag-ID cache eviction hooks so flag writes in
// this process invalidate cached (project,key)->flagID mappings.
// It swaps idManager onto the durable PB-backed salt Store so same-day
// anonIDs survive restarts; unit tests that never call Register keep
// the MemoryStore default (see SetIdentityManager).
func Register(se *core.ServeEvent) {
	idManager = identity.NewManager(identity.NewPBStore(se.App))
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
	anonID, country, derr := deriveIdentity(re, env.Id, now)
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
		// anonID-only storage: a client ?userHash= query value of any shape
		// is accepted-but-ignored; the server-derived daily anonID always
		// wins, so fetch bucketing and exposures join on the same value
		// (stable ?b= excepted — it wins verbatim on both paths).
		// Per-event body userHash stays accepted-but-ignored;
		// userHash column stores empty.
		stored = append(stored, StoredEvent{
			EnvID: env.Id,
			Kind:  ev.Kind, Flag: flagIDs[ev.Flag], Variant: ev.Variant,
			AnonID: anonID, Country: country, Ts: ts,
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
