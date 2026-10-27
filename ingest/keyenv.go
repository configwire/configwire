package ingest

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/limits"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// KeyEnvKind selects which per-second rate-limit bucket RequireKeyEnv
// enforces after key auth + env resolution. KindStream applies no
// per-second bucket: stream concurrency is capped separately in the
// stream handler (maxStreamsPerKey), so sharing a bucket here would add
// a new limit stream never had.
type KeyEnvKind int

const (
	// KindFetch enforces the fetch bucket (AllowFetchKey + EffectiveFetchRps).
	KindFetch KeyEnvKind = iota + 1
	// KindIngest enforces the ingest bucket (AllowIngestKey + EffectiveIngestRps).
	KindIngest
	// KindStream enforces no per-second bucket (key + env only).
	KindStream
)

// RequireKeyEnv is the shared SDK-key preamble for the fetch, ingest,
// and stream handlers: RequireSDKKey (401, or ErrKeyResponded when the
// 429/503 was already written) -> ResolveForKey (404/400/401 via
// ToRequestError) -> per-kind per-second 429 with Retry-After: 1.
//
// Unified order is key -> resolve -> 429 (the ingest order): unknown
// slugs answer 404 even when the key's budget is exhausted. Fetch
// previously rate-limited before resolving; its 429 now moves after
// env resolution, matching its documented 401 -> 404 order.
//
// Dual-serve: legacy /api/v1/env/{env}/* routes carry a non-empty
// {env} slug validated against the key's bound env via
// envresolve.ResolveForKey (mismatch 401 wrong-env, ambiguous slug 400
// where reachable); key-only aliases carry no slug and resolve purely
// from the key's bound env record (missing row 404 unknown-env,
// empty/dangling binding 401 wrong-env). A client-sent ?env= query
// value is accepted-but-ignored at the handler layer (never read here,
// never validated).
//
// CORS/Vary stamping stays per-handler (fetch sets its own Vary +
// If-None-Match variant, stream sets Vary:X-ConfigWire-Key) and is NOT
// shared here — only key + resolve + 429 are.
//
// Contract: when the response was already written (RequireSDKKey's
// saturation/storage paths, or the 429 below), the returned error is
// ErrKeyResponded and both records are nil — callers must return nil,
// NOT propagate it. Resolve failures return the mapped request error
// (non-nil record absent). Callers keep their existing
// errors.Is(err, ErrKeyResponded) early-return check unchanged.
func RequireKeyEnv(re *core.RequestEvent, kind KeyEnvKind) (*core.Record, *core.Record, error) {
	key, err := RequireSDKKey(re)
	if err != nil {
		return nil, nil, err
	}
	slug := re.Request.PathValue("env")
	var env *core.Record
	if slug != "" {
		var rerr error
		env, rerr = resolveLegacy(re, key, slug)
		if rerr != nil {
			return nil, nil, rerr
		}
	} else {
		var rerr error
		env, rerr = resolveKeyOnly(re, key)
		if rerr != nil {
			return nil, nil, rerr
		}
	}
	var allowed bool
	switch kind {
	case KindFetch:
		allowed = limits.AllowFetchKey(key.Id, limits.EffectiveFetchRps(key))
	case KindIngest:
		allowed = limits.AllowIngestKey(key.Id, limits.EffectiveIngestRps(key))
	case KindStream:
		return key, env, nil
	default:
		return key, env, nil
	}
	if !allowed {
		denyWrite(re, "rate-limited", 429, KeyPrefix(re.Request.Header.Get(HeaderKey)), "per-key")
		if werr := apireq.TooMany(re, "Rate limit exceeded."); werr != nil {
			return nil, nil, werr
		}
		return nil, nil, ErrKeyResponded
	}
	return key, env, nil
}

// resolveLegacy validates a non-empty legacy {env} slug against the key's
// bound env. Deterministic: the key's env IS the env, so a slug shared by
// several projects can never misroute here.
func resolveLegacy(re *core.RequestEvent, key *core.Record, slug string) (*core.Record, error) {
	env, err := envresolve.ResolveForKey(re.App, slug, key.GetString("env"))
	if err != nil {
		prefix := KeyPrefix(re.Request.Header.Get(HeaderKey))
		reason, status := "wrong-env", http.StatusUnauthorized
		switch {
		case errors.Is(err, envresolve.ErrNotFound):
			reason, status = "unknown-env", http.StatusNotFound
		case errors.Is(err, envresolve.ErrScopeMismatch):
			reason, status = "wrong-env", http.StatusUnauthorized
		default:
			var amb envresolve.AmbiguousError
			if errors.As(err, &amb) {
				reason, status = "ambiguous-env", http.StatusBadRequest
			} else {
				reason, status = "resolve-error", http.StatusInternalServerError
			}
		}
		denyWrite(re, reason, status, prefix, "")
		return nil, envresolve.ToRequestError(re, err)
	}
	return env, nil
}

// resolveKeyOnly resolves the env purely from the key's bound env record
// for the key-only aliases (no {env} segment). A missing env row answers
// 404 unknown-env; an empty/dangling binding answers 401 wrong-env; a
// storage failure answers 500 resolve-error (never 404 — a missing row
// must not read as an outage and vice versa). The not-found check is
// errors.Is(err, sql.ErrNoRows): PocketBase's dbx One() returns it for an
// empty result set (same discriminator the sso redeem path uses for
// FindFirstRecordByData). This deliberately differs from mintInputError's
// any-error→404 precedent, which conflates the two on an admin-only path
// (admin + legacy slug paths keep that shape — untouched, out of scope).
func resolveKeyOnly(re *core.RequestEvent, key *core.Record) (*core.Record, error) {
	keyEnvID := key.GetString("env")
	var resolveErr error
	var env *core.Record
	if keyEnvID == "" {
		resolveErr = envresolve.ErrScopeMismatch
	} else if rec, ferr := re.App.FindRecordById("environments", keyEnvID); ferr != nil {
		if !errors.Is(ferr, sql.ErrNoRows) {
			prefix := KeyPrefix(re.Request.Header.Get(HeaderKey))
			denyWrite(re, "resolve-error", http.StatusInternalServerError, prefix, "")
			if werr := apireq.Internal(re, "ingest: failed to resolve env", ferr); werr != nil {
				return nil, werr
			}
			return nil, ErrKeyResponded
		}
		resolveErr = envresolve.ErrNotFound
	} else {
		env = rec
	}
	if resolveErr != nil {
		prefix := KeyPrefix(re.Request.Header.Get(HeaderKey))
		reason, status := "wrong-env", http.StatusUnauthorized
		switch {
		case errors.Is(resolveErr, envresolve.ErrNotFound):
			reason, status = "unknown-env", http.StatusNotFound
		case errors.Is(resolveErr, envresolve.ErrScopeMismatch):
			reason, status = "wrong-env", http.StatusUnauthorized
		default:
			reason, status = "resolve-error", http.StatusInternalServerError
		}
		denyWrite(re, reason, status, prefix, "")
		return nil, envresolve.ToRequestError(re, resolveErr)
	}
	return env, nil
}
