// Package ingest implements the analytics events write path.
//
// Accepted JSON (defined here; the experiment variant overlay lives in
// configwire/eval/experiment.go):
//
//	POST /api/v1/env/:env/events
//	Header: X-ConfigWire-Key: <full sdk key>
//	Body: {"events":[{"kind":"fetch|exposure","variant":"<name>",
//	    "userHash":"<opaque>", "ts":"2026-09-22T00:00:00Z | 1726870000"}]}
//	variant/userHash/ts are all optional; ts accepts RFC3339 string or
//	unix-seconds number, absent means server time. Raw "userId"/"ip" keys are
//	STRICTLY REJECTED (400) — PII must never reach storage.
//
// SDK key format (contract for T10/T11/T16):
//   - The full key is an opaque string presented in X-ConfigWire-Key.
//   - sdk_keys rows store prefix = first 8 chars of the full key (fast
//     prefilter) and hash = lowercase hex(sha256(fullKey)) (constant-time
//     comparison). The full key is never stored.
//   - Unknown, mismatched, revoked, or env-mismatched keys → 401.
//   - Rate limiting is per-second per-key: sdk_keys.fetchRps/ingestRps
//     requests per 1s window via configwire/limits (EffectiveIngestRps,
//     missing/zero falls back to the global ingest budget). Exhausted → 429.
//     Every authenticated hit to this endpoint consumes one token,
//     including requests later rejected as 400 (documented choice:
//     simplest accounting, no free probing).
//   - Batching: valid events enqueue to a buffered channel and the handler
//     returns 202 immediately. A background goroutine flushes every 1s or
//     every 500 rows (whichever first) via app.Save, so worst-case ingest
//     lag is ~1s + write time (bounded ≤2s). Batch sizes >100 → 400.
package ingest

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	// HeaderKey carries the opaque SDK key; RequireSDKKey reads only this header.
	HeaderKey = "X-ConfigWire-Key"
	// MaxBatchEvents bounds a single request body; larger → 400.
	MaxBatchEvents = 100
	// MaxBodyBytes bounds the whole request body; larger → 413.
	MaxBodyBytes = 128 << 10
	// MaxEventBytes bounds a single event object; larger → 413.
	MaxEventBytes = 64 << 10
	// MaxVariantLen bounds variant names in runes (chars); over-limit → 400.
	MaxVariantLen = 64
	// MaxUserHashLen bounds opaque user hashes in runes (chars); over-limit → 400.
	MaxUserHashLen = 128
	// MaxFutureSkew tolerates clock skew for future ts; beyond → 400.
	MaxFutureSkew = 5 * time.Minute
	// MaxPastAge bounds how far back ts may lie; older → 400.
	MaxPastAge = 90 * 24 * time.Hour
)

// EventIn is one validated ingest event with server-normalized timestamp.
type EventIn struct {
	Kind     string
	Variant  string
	UserHash string
	Version  int
	Ts       time.Time // zero when absent (handler substitutes time.Now)
}

// apiError carries an HTTP status through validation so the handler can
// respond without branching on message strings.
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func badRequest(msg string) *apiError {
	return &apiError{Status: http.StatusBadRequest, Msg: msg}
}

func entityTooLarge(msg string) *apiError {
	return &apiError{Status: http.StatusRequestEntityTooLarge, Msg: msg}
}

// rawEvent mirrors the wire shape; Ts stays raw for strict parsing and
// UserID/IP are deliberately absent — their presence is detected on the
// raw map first (strict reject, never decoded into a storable field).
type rawEvent struct {
	Kind     string          `json:"kind"`
	Variant  string          `json:"variant"`
	UserHash string          `json:"userHash"`
	Version  int             `json:"version"`
	Ts       json.RawMessage `json:"ts"`
}

// ValidateBody parses and validates a request body purely (no I/O), so it is
// unit-testable. It enforces: non-empty JSON with an "events" array,
// 1..MaxBatchEvents items, per-event ≤ MaxEventBytes, kind ∈
// {fetch,exposure}, no raw "userId"/"ip" keys, parseable ts.
func ValidateBody(body []byte) ([]EventIn, *apiError) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, badRequest("empty body: expected {\"events\":[...]}.")
	}
	var top struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, badRequest("malformed JSON: " + err.Error())
	}
	if top.Events == nil {
		return nil, badRequest("missing required field \"events\".")
	}
	if len(top.Events) == 0 {
		return nil, badRequest("empty batch: at least one event is required.")
	}
	if len(top.Events) > MaxBatchEvents {
		return nil, badRequest("batch too large: max 100 events per request.")
	}
	out := make([]EventIn, 0, len(top.Events))
	for i, raw := range top.Events {
		ev, aerr := validateRawEvent(raw)
		if aerr != nil {
			aerr.Msg = jsonErrorPrefix(i) + aerr.Msg
			return nil, aerr
		}
		out = append(out, ev)
	}
	return out, nil
}

func jsonErrorPrefix(i int) string {
	return "events[" + strconv.Itoa(i) + "]: "
}

func validateRawEvent(raw json.RawMessage) (EventIn, *apiError) {
	var ev EventIn
	if len(raw) > MaxEventBytes {
		return ev, entityTooLarge("event exceeds 64KB.")
	}
	// Strict PII guard: detect raw userId/ip keys on the untyped map so
	// that even null-valued keys are rejected (presence, not value).
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ev, badRequest("malformed event JSON: " + err.Error())
	}
	if _, ok := m["userId"]; ok {
		return ev, badRequest("raw \"userId\" is forbidden: send \"userHash\" instead.")
	}
	if _, ok := m["ip"]; ok {
		return ev, badRequest("raw \"ip\" is forbidden and never stored.")
	}
	var r rawEvent
	if err := json.Unmarshal(raw, &r); err != nil {
		return ev, badRequest("malformed event JSON: " + err.Error())
	}
	if r.Kind != "fetch" && r.Kind != "exposure" {
		return ev, badRequest("invalid kind: must be \"fetch\" or \"exposure\".")
	}
	if len([]rune(r.Variant)) > MaxVariantLen {
		return ev, badRequest("variant too long: max 64 chars.")
	}
	if len([]rune(r.UserHash)) > MaxUserHashLen {
		return ev, badRequest("userHash too long: max 128 chars.")
	}
	if strings.IndexByte(r.Variant, 0) >= 0 || strings.IndexByte(r.UserHash, 0) >= 0 {
		return ev, badRequest("NUL byte in variant/userHash is forbidden.")
	}
	if r.Version < 0 {
		return ev, badRequest("invalid version: must be >= 0.")
	}
	ts, aerr := parseTs(r.Ts)
	if aerr != nil {
		return ev, aerr
	}
	ev = EventIn{Kind: r.Kind, Variant: r.Variant, UserHash: r.UserHash, Version: r.Version, Ts: ts}
	return ev, nil
}

// parseTs accepts absent/null (zero time = server time), RFC3339 strings,
// and unix-seconds numbers. Out-of-window non-zero ts → 400.
func parseTs(raw json.RawMessage) (time.Time, *apiError) {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return time.Time{}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		parsed, perr := time.Parse(time.RFC3339Nano, s)
		if perr != nil {
			if parsed, perr = time.Parse(time.RFC3339, s); perr != nil {
				return time.Time{}, badRequest("invalid ts: must be RFC3339 or unix seconds.")
			}
		}
		return clampTs(parsed.UTC())
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		sec, nsec := int64(f), int64((f-float64(int64(f)))*1e9)
		return clampTs(time.Unix(sec, nsec).UTC())
	}
	return time.Time{}, badRequest("invalid ts: must be RFC3339 or unix seconds.")
}

func clampTs(ts time.Time) (time.Time, *apiError) {
	now := time.Now().UTC()
	if ts.After(now.Add(MaxFutureSkew)) {
		return time.Time{}, badRequest("invalid ts: timestamp too far in the future.")
	}
	if ts.Before(now.Add(-MaxPastAge)) {
		return time.Time{}, badRequest("invalid ts: timestamp too far in the past.")
	}
	return ts, nil
}

// ResolveUserHash picks the storable hash: an explicit userHash wins;
// otherwise a (server-side only) userID is hashed; neither → "".
// NOTE: the HTTP path rejects raw userId before this is ever reached, so the
// userID branch exists for completeness/testability and future server-side
// callers — raw PII from clients is never stored.
func ResolveUserHash(userHash, userID string) string {
	if userHash != "" {
		return userHash
	}
	if userID != "" {
		return HashUser(userID)
	}
	return ""
}

var (
	fastCacheMu sync.RWMutex
	fastCache   = make(map[string]string)
	slowSem     = make(chan struct{}, 4)
	// slowVerifyCalls counts bcrypt verifications (test hook only).
	slowVerifyCalls int
)

const fastCacheCap = 10000

// ResetKeyCache clears the process-local fast cache (tests only).
func ResetKeyCache() {
	fastCacheMu.Lock()
	fastCache = make(map[string]string)
	slowVerifyCalls = 0
	fastCacheMu.Unlock()
}

// SlowVerifyCalls reports bcrypt verification count (tests only).
func SlowVerifyCalls() int {
	fastCacheMu.RLock()
	defer fastCacheMu.RUnlock()
	return slowVerifyCalls
}

func cacheGet(fast string) (string, bool) {
	fastCacheMu.RLock()
	id, ok := fastCache[fast]
	fastCacheMu.RUnlock()
	return id, ok
}

func cachePut(fast, id string) {
	fastCacheMu.Lock()
	if len(fastCache) >= fastCacheCap {
		for k := range fastCache {
			delete(fastCache, k)
			if len(fastCache) < fastCacheCap/2 {
				break
			}
		}
	}
	fastCache[fast] = id
	fastCacheMu.Unlock()
}

func cacheDel(fast string) {
	fastCacheMu.Lock()
	delete(fastCache, fast)
	fastCacheMu.Unlock()
}

// EvictKeyByID removes every fast-cache entry bound to the given sdk_keys
// record id. The map is small (cap 10000) and this runs only on admin
// edits/deletes, so a linear scan is fine. Nil/empty-safe: no-op on "".
func EvictKeyByID(id string) {
	if id == "" {
		return
	}
	fastCacheMu.Lock()
	for k, v := range fastCache {
		if v == id {
			delete(fastCache, k)
		}
	}
	fastCacheMu.Unlock()
}

// findSDKKey returns the sdk_keys row matching the full SDK key, or
// (nil, nil) when no row matches. The prefix prefilter runs in the DB
// instead of a full-table scan; rows with a verifier slow-verify under
// the CPU-DoS semaphore. Successes populate the fast cache keyed by the
// fast hash. Revocation is NOT checked here — the caller maps revoked
// rows to 401, preserving auth order.
//
// Deprecated (remove in v0.2.0): rows without a verifier fall back to
// the legacy constant-time fast-hash compare, kept only so pre-existing
// v1 keys keep authenticating. New rows always store a verifier.
func findSDKKey(app core.App, full string) (*core.Record, error) {
	prefix := KeyPrefix(full)
	want := KeyHash(full)
	if id, ok := cacheGet(want); ok {
		rec, err := app.FindRecordById("sdk_keys", id)
		if err != nil || rec == nil {
			cacheDel(want)
			return nil, nil
		}
		return rec, nil
	}
	recs, err := app.FindAllRecords("sdk_keys", dbx.HashExp{"prefix": prefix})
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if v := r.GetString("verifier"); v != "" {
			select {
			case slowSem <- struct{}{}:
			default:
				return nil, nil
			}
			fastCacheMu.Lock()
			slowVerifyCalls++
			fastCacheMu.Unlock()
			ok := VerifySlow(v, full)
			<-slowSem
			if ok {
				cachePut(want, r.Id)
				return r, nil
			}
			continue
		}
		// Deprecated (remove in v0.2.0): legacy v1 row — fast-hash compare
		// kept only so pre-existing keys keep authenticating.
		if subtle.ConstantTimeCompare([]byte(r.GetString("hash")), []byte(want)) != 1 {
			continue
		}
		cachePut(want, r.Id)
		return r, nil
	}
	return nil, nil
}

// RequireSDKKey authenticates X-ConfigWire-Key against sdk_keys (exported for
// T10 reuse). Lookup prefilters on prefix (first 8 chars, DB-side), then
// slow-verifies the bcrypt verifier (or the deprecated legacy fast hash).
// Unknown/missing/revoked → 401 error suitable for returning directly from
// a handler.
func RequireSDKKey(re *core.RequestEvent) (*core.Record, error) {
	denied := func() (*core.Record, error) {
		return nil, re.UnauthorizedError("Missing or invalid SDK key.", nil)
	}
	full := re.Request.Header.Get(HeaderKey)
	if full == "" {
		return denied()
	}
	key, err := findSDKKey(re.App, full)
	if err != nil {
		return denied()
	}
	if key == nil || key.GetBool("revoked") {
		return denied()
	}
	return key, nil
}

// readBody caps the request body purely at the HTTP layer: over-limit → 413.
func readBody(re *core.RequestEvent) ([]byte, *apiError) {
	body, err := io.ReadAll(io.LimitReader(re.Request.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, badRequest("unreadable body: " + err.Error())
	}
	if len(body) > MaxBodyBytes {
		return nil, entityTooLarge("body exceeds 128KB.")
	}
	return body, nil
}
