package ingest

import (
	"errors"
	"net/http"

	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/limits"

	"github.com/pocketbase/pocketbase/core"
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
	// Deterministic: the key's env IS the env, so a slug shared by
	// several projects can never misroute here.
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
		return nil, nil, envresolve.ToRequestError(re, err)
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
