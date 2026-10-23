// Package fetch implements the SDK delivery endpoint.
//
//	GET /api/v1/env/:env/config?platform=&appVersion=&locale=&country=&userHash=&attrs=<json>&exp=<status>
//
// FETCH CONTRACT:
//   - Auth: X-ConfigWire-Key via ingest.RequireSDKKey reuse. Unknown, missing,
//     revoked, or env-mismatched keys -> 401. SDK keys and userIDs are never
//     logged (hashes only); evaluation cost is O(flags + rules) per request.
//   - Env: resolved deterministically from the key's env (the key's env
//     IS the env, so a slug shared by several projects can never
//     misroute). Unknown slug -> 404. Order is 401 (key) -> 404/400
//     (env) -> 401 (key not scoped to this env) -> 304/200.
//   - Empty release: an env with NO release row yet -> 200 with
//     {"version":0,"etag":"none","values":{},"variants":{},"fetchAt":...}.
//     Never 404, so fresh envs boot clean. values is EMPTY (not flag
//     defaults): the admin has published nothing, so there is nothing to
//     serve; SDKs fall back to their in-app defaults.
//   - Latest release: the row with max version for the env. Its STORED etag
//     and STORED snapshot bytes are served VERBATIM (never recomputed —
//     the etag input was the canonical pre-save bytes, see
//     releases.EtagFor). Response headers carry ETag: <etag>.
//   - 304: If-None-Match == stored etag (EXACT string match only; a "W/"-
//     prefixed value does NOT match) -> 304 with an empty body.
//   - Evaluation: for each snapshot flag, eval.Flag/Rules are rebuilt from
//     the stored snapshot and evaluated with eval.Evaluate; flags targeted
//     by a RUNNING experiment are overlaid via eval.EvaluateWithExperiment
//     (variant recorded in the "variants" map). Flags with no experiment
//     targeting them have NO variants entry. Response:
//     {"version","etag","values":{key:typed},"variants":{key:variant},
//     "fetchAt":rfc3339}. A flag whose snapshot default has an unexpected
//     shape is served as-is (publish-time validation already coerced it).
//   - Context: bucketing identity is the honored client hash when
//     ?userHash= matches ^[0-9a-f]{16}$ (identity.IsValidAnonID),
//     else the server-derived anonID via identity.Manager.AnonID
//     (envID, ClientIP, User-Agent, day), stored in Context.UserHash
//     (field name kept for eval.Bucket compat); country is server geo
//     from identity.CountryFromHeaders only — client ?country= is
//     accepted-but-ignored. Malformed/absent ?userHash=
//     falls back silently (never 400). platform/appVersion/
//     locale pass through (appVersion=not-semver -> condition false ->
//     defaults fallthrough, still 200, never 400), attrs=<json> parsed as a
//     flat map for custom.* conditions. ?uid= is sunset: any trimmed
//     non-empty value is 400 (send userHash instead). Forbidden PII attrs
//     keys (email and device identifiers, case-insensitive) are 400;
//     values are never logged.
//   - attrs error policy: malformed attrs (invalid JSON, or valid JSON that
//     is not an object) -> 400 documented; attrs longer than 8192 bytes ->
//     414 documented; forbidden PII keys -> 400 documented. Missing attrs
//     -> empty custom map (anonymous attrs).
//   - exp override: ?exp= is ignored unless CONFIGWIRE_ENABLE_EXP_OVERRIDE==1.
//   - Gzip: route binds apis.Gzip(), so Accept-Encoding: gzip responses
//     carry Content-Encoding: gzip.
//   - CORS: every fetch response (including 304) sets
//     Access-Control-Allow-Origin, default "*" for Flutter/web dev. Restrict
//     in production with env CONFIGWIRE_CORS_ORIGIN=https://app.example.com
//     (single origin, no paid infra, simple header). An OPTIONS preflight
//     route answers 204 with Allow-Origin/Methods/Headers.
package fetch

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/identity"
	"github.com/configwire/configwire/ingest"
	"github.com/configwire/configwire/limits"
	"github.com/configwire/configwire/releases"
	"github.com/configwire/configwire/security"
	"github.com/pocketbase/dbx"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// MaxAttrsBytes bounds the raw ?attrs= parameter; longer -> 414.
const MaxAttrsBytes = 8192

// emptyReleaseEtag is served when an env has no release row yet.
const emptyReleaseEtag = "none"

const allowHeaders = "X-ConfigWire-Key, If-None-Match"

// attrsForbidden holds lowercased top-level custom-attr keys that are
// never accepted (PII/device identifiers). Matched case-insensitively;
// values are never logged, errors name only the key (PII-free).
var attrsForbidden = map[string]struct{}{
	"email": {}, "e-mail": {}, "phone": {}, "ip": {},
	"deviceid": {}, "device_id": {}, "advertisingid": {},
	"adid": {}, "idfa": {}, "idfv": {},
	"androidid": {}, "serial": {}, "mac": {}, "imei": {},
}

// isForbiddenAttr reports whether a top-level custom-attr key is denied
// (case-insensitive match against attrsForbidden).
func isForbiddenAttr(key string) bool {
	_, ok := attrsForbidden[strings.ToLower(key)]
	return ok
}

// deprecatedSnapshotTotal counts process-local legacy-shape snapshot serves.
//
// Deprecated: legacy top-level snapshot compat is deprecated and will be
// removed in the next server major (republish to migrate); the counter
// goes away with it.
var deprecatedSnapshotTotal atomic.Uint64

// markDeprecatedSnapshot stamps the compat header + PII-free log/counter.
// It takes the header (not the request event) so the shared stamping logic
// stays unit-testable; callers pass re.Response.Header(). Never logs
// snapshot contents (counts/shape only).
//
// Deprecated: legacy snapshot compat is deprecated and will be removed in
// the next server major; do not call from new code.
func markDeprecatedSnapshot(h http.Header) {
	h.Set("X-CW-Deprecated-snapshot", "true")
	deprecatedSnapshotTotal.Add(1)
	log.Printf("fetch: deprecated legacy snapshot served (compat window; republish to migrate)")
}

// setFetchCacheHeaders stamps private cache headers on fetch 200/304
// responses. Private because responses vary per SDK key + encoding.
func setFetchCacheHeaders(re *core.RequestEvent) {
	h := re.Response.Header()
	h.Set("Vary", "X-ConfigWire-Key, Accept-Encoding")
	h.Set("Cache-Control", "private, max-age=0")
}

func corsOrigin() string {
	if v := os.Getenv("CONFIGWIRE_CORS_ORIGIN"); v != "" {
		return v
	}
	return "*"
}

// Register mounts the SDK delivery endpoint with gzip encoding.
// It swaps anonManager onto the durable PB-backed salt Store so
// same-day anonIDs survive restarts; unit tests that never call
// Register keep the MemoryStore default (see SetAnonManager).
func Register(se *core.ServeEvent) {
	anonManager = identity.NewManager(identity.NewPBStore(se.App))
	se.Router.GET("/api/v1/env/{env}/config", getConfig).Bind(apis.Gzip())
	se.Router.OPTIONS("/api/v1/env/{env}/config", optionsConfig)
}

// optionsConfig answers the CORS preflight for fetch (no auth: preflights
// carry no credentials by spec).
func optionsConfig(re *core.RequestEvent) error {
	security.SetHeaders(re)
	h := re.Response.Header()
	h.Set("Access-Control-Allow-Origin", corsOrigin())
	h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	h.Set("Access-Control-Allow-Headers", allowHeaders)
	h.Set("Access-Control-Max-Age", "86400")
	return re.NoContent(http.StatusNoContent)
}

// anonManager derives per-request anonIDs via per-env daily salts.
// MemoryStore is the in-process default until the identity_salts
// collection is wired as the durable Store; salts never leave the
// identity package and raw IPs/user-agents are never persisted.
var anonManager = identity.NewManager(identity.NewMemoryStore())

// SetAnonManager swaps the anonID manager (tests only).
func SetAnonManager(m *identity.Manager) {
	if m != nil {
		anonManager = m
	}
}

// ParseBucket validates the stable-bucket ?b= query param (clean seam for
// the T2 mint branch and the T7 events/ingest ?b= honor — both reuse this
// rule so fetch arms and event attribution stay joined).
//
// Thin delegate over identity.ParseBucketParam, the single canonical rule
// (plain int string "0"-"9999", no coercion). Returns ("", nil) when
// absent, (canonical, nil) when valid, ("", error) when malformed
// (caller maps to 400, mirroring the ?uid= sunset).
func ParseBucket(q url.Values) (string, error) {
	return identity.ParseBucketParam(q.Get("b"))
}

// effectiveUserHash resolves one fetch's bucketing identity with the
// stable-bucket precedence chain: valid ?b= > valid ?userHash= > daily
// anonID fallback. A valid ?b= arm wins even alongside a valid
// ?userHash= (one-time reshuffle for upgraded seeded installs: an
// install that previously bucketed on its honored seed hash re-rolls
// onto its minted number once it starts sending ?b= — accepted by
// owner). A malformed ?b= falls through here so BuildContext owns the
// single 400 (this helper never errors, preserving the silent-fallback
// contract for ?userHash=); see identity.ResolveHonoredHash.
func effectiveUserHash(q url.Values, anonID string) string {
	if b, err := ParseBucket(q); err == nil && b != "" {
		return b
	}
	return identity.ResolveHonoredHash(q.Get("userHash"), anonID)
}

// MintBucket mints one stable-bucket number as its canonical string
// "0"-"9999", uniform over 10000 via crypto/rand (stdlib only, no seed
// state — mirrors identity.GenerateSalt's rand.Read discipline, bounded
// with rand.Int). Stateless per-response randomness: the server persists
// nothing; first-write-wins persistence lives client-side (T5).
func MintBucket() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(n.Int64(), 10), nil
}

// EffectiveBucket resolves the response bucket field: the client's valid
// ?b= wins verbatim (no mint override), else a fresh MintBucket. Malformed
// ?b= returns the single ParseBucket 400-mapped error (unreachable from
// getConfig — BuildContext already owns that 400 — but kept so this seam
// never invents a second validation rule).
func EffectiveBucket(q url.Values) (string, error) {
	if b, err := ParseBucket(q); err != nil {
		return "", err
	} else if b != "" {
		return b, nil
	}
	return MintBucket()
}

// emptyReleaseBody builds the fresh-env 200 shape: version 0, etag "none",
// EMPTY values — and NO bucket field (nothing to arm yet).
func emptyReleaseBody(fetchAt string) map[string]any {
	return map[string]any{
		"version":  0,
		"etag":     emptyReleaseEtag,
		"values":   map[string]any{},
		"variants": map[string]string{},
		"fetchAt":  fetchAt,
	}
}

// evaluatedBody builds the evaluated 200 shape: version/etag/values/
// variants plus the EFFECTIVE bucket (client's valid ?b= else fresh mint).
// The 304 path returns empty before this is ever called, so a 304 never
// mints.
func evaluatedBody(version int, etag string, values map[string]any, variants map[string]string, bucket, fetchAt string) map[string]any {
	return map[string]any{
		"version":  version,
		"etag":     etag,
		"values":   values,
		"variants": variants,
		"bucket":   bucket,
		"fetchAt":  fetchAt,
	}
}

// BuildContext maps fetch query params onto an eval.Context purely (no I/O),
// so it is unit-testable. Bucketing identity is the caller-resolved value:
// getConfig passes effectiveUserHash(q, anonID) as anonID and it is stored
// in Context.UserHash (field name kept for eval.Bucket compat); country is
// the server geo from the limits country-header order (passed in by
// getConfig). Client ?country= is accepted-but-ignored and never reaches
// eval. Legacy ?uid= is sunset: any trimmed non-empty ?uid= is a
// 400 ("raw \"uid\" is forbidden: send \"userHash\" instead."), even
// alongside anonID; empty ?uid= == absent. platform/appVersion/locale
// pass through untouched (invalid values evaluate to false downstream,
// never 400 here). Malformed attrs, forbidden PII attrs keys, or attrs
// over MaxAttrsBytes -> error (caller maps to 400/414). Missing
// attrs -> anonymous customs.
func BuildContext(q url.Values, anonID, country string) (eval.Context, error) {
	if strings.TrimSpace(q.Get("uid")) != "" {
		return eval.Context{}, fmt.Errorf(`raw "uid" is forbidden: send "userHash" instead.`)
	}
	// Stable bucket wins over the caller-resolved identity (which already
	// encodes honored-?userHash= > anonID): ctx.UserHash carries the
	// canonical number string so eval.Bucket(number+seed) evaluates the
	// minted arm. Malformed ?b= is 400 here (single error owner;
	// effectiveUserHash falls through on malformed so this branch owns
	// it). ?uid= above wins over ?b= (uid checked first).
	userHash := anonID
	if b, berr := ParseBucket(q); berr != nil {
		return eval.Context{}, berr
	} else if b != "" {
		userHash = b
	}
	ctx := eval.Context{
		UserHash:    userHash,
		Platform:    q.Get("platform"),
		AppVersion:  q.Get("appVersion"),
		Locale:      q.Get("locale"),
		Country:     country,
		CustomAttrs: map[string]any{},
	}
	raw := q.Get("attrs")
	if raw == "" {
		return ctx, nil
	}
	if len(raw) > MaxAttrsBytes {
		return ctx, fmt.Errorf("attrs too large: max %d bytes", MaxAttrsBytes)
	}
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return ctx, fmt.Errorf("malformed attrs: not a JSON object")
	}
	if m == nil {
		return ctx, fmt.Errorf("malformed attrs: not a JSON object")
	}
	for k := range m {
		if isForbiddenAttr(k) {
			return ctx, fmt.Errorf("attrs key %q is forbidden: remove PII custom attributes", k)
		}
	}
	ctx.CustomAttrs = m
	return ctx, nil
}

// expOverride returns ?exp= only when CONFIGWIRE_ENABLE_EXP_OVERRIDE==1, else "".
func expOverride(q url.Values) string {
	if os.Getenv("CONFIGWIRE_ENABLE_EXP_OVERRIDE") != "1" {
		return ""
	}
	return q.Get("exp")
}

// EtagMatches reports whether an If-None-Match header exactly equals the
// stored etag. Exact match only: weak ("W/"-prefixed) or quoted values do
// NOT match (stored etags are bare hex, never quoted).
func EtagMatches(ifNoneMatch, etag string) bool {
	return ifNoneMatch != "" && ifNoneMatch == etag
}

// EvaluateSnapshot evaluates every flag in a stored snapshot for ctx purely
// (no I/O), so it is unit-testable with a fake latest-release input.
// Both stored shapes reach here identically: Snapshot.UnmarshalJSON folds
// legacy top-level experiments into their owning flag (orphans dropped),
// and flattenForEval merges per-flag nested rows with the decoded
// flattened set (nested wins, orphans dropped), so a nested snapshot and
// its legacy twin evaluate to the same values and variants.
// Returns values (typed per flag) and variants (one entry per flag targeted
// by at least one experiment row; the assigned variant name, including the
// DefaultVariant fallthrough for non-running/anonymous paths).
// statusOverride, when non-empty, replaces each experiment's stored status
// (the ?exp= query param); otherwise the stored status is used.
func EvaluateSnapshot(snap releases.Snapshot, ctx eval.Context, statusOverride string) (map[string]any, map[string]string) {
	all := flattenForEval(snap)
	values := make(map[string]any, len(snap.Flags))
	variants := map[string]string{}
	for _, sf := range snap.Flags {
		flag := eval.Flag{Key: sf.Key, Type: sf.Type, Default: sf.Default}
		rules := make([]eval.Rule, 0, len(sf.Rules))
		for i, sr := range sf.Rules {
			if r, ok := decodeRule(sr, i); ok {
				rules = append(rules, r)
			}
		}
		exps := expsForFlag(all, sf.Key)
		if len(exps) == 0 {
			values[sf.Key] = eval.Evaluate(flag, rules, ctx)
			continue
		}
		// First matching experiment row wins (per-flag order is by seed
		// with id tiebreak, deterministic from the publish path).
		se := exps[0]
		status := se.Status
		if statusOverride != "" {
			status = statusOverride
		}
		v, variant := eval.EvaluateWithExperiment(flag, rules, ctx, decodeExperiment(se), status)
		values[sf.Key] = v
		variants[sf.Key] = variant
	}
	return values, variants
}

// decodeRule converts one stored snapshot rule to an eval.Rule. The stored
// condition is either a single object (wrapped as one eval.Condition,
// backward compat) or a non-empty array of objects (decoded as N
// eval.Conditions, ANDed by eval.ruleMatches). A null or undecodable
// condition is FAIL-CLOSED (ok=false): the rule is dropped from the eval
// set so it matches nobody. Publish-time validation is the primary gate;
// this is the backstop for legacy/corrupt rows.
func decodeRule(sr releases.SnapshotRule, i int) (eval.Rule, bool) {
	r := eval.Rule{ID: fmt.Sprintf("rule-%d", i)}
	if sr.Condition == nil {
		return eval.Rule{}, false
	}
	raw, err := json.Marshal(sr.Condition)
	if err != nil {
		return eval.Rule{}, false
	}
	var arr []eval.Condition
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) >= 1 {
		r.Conditions = arr
		r.Value = sr.Value
		return r, true
	}
	var c eval.Condition
	if err := json.Unmarshal(raw, &c); err != nil {
		return eval.Rule{}, false
	}
	r.Conditions = []eval.Condition{c}
	r.Value = sr.Value
	return r, true
}

// flattenForEval collects the eval-ready experiment set: per-flag nested
// entries first with the parent key stamped (parent wins on identity),
// then rows from the decoded flattened set whose flag matches a snapshot
// flag (legacy top-level fallback; orphans dropped — the same exclusion
// ResolveExperimentFlag applies at build time to untargeted rows).
// Values decoded via Snapshot.UnmarshalJSON carry both from the same
// rows, so the merge dedupes to identical rows for either stored shape;
// hand-built values with only nested rows work unchanged. Record ids are
// globally unique; id-less rows fall back to parent flag + seed.
func flattenForEval(snap releases.Snapshot) []releases.SnapshotExperiment {
	keys := make(map[string]bool, len(snap.Flags))
	for _, sf := range snap.Flags {
		keys[sf.Key] = true
	}
	keyOf := func(e releases.SnapshotExperiment) string {
		if e.ID != "" {
			return "id\x00" + e.ID
		}
		return "seed\x00" + e.Flag + "\x00" + e.Seed
	}
	var out []releases.SnapshotExperiment
	seen := map[string]bool{}
	for _, sf := range snap.Flags {
		for _, e := range sf.Experiments {
			e.Flag = sf.Key
			k := keyOf(e)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, e)
		}
	}
	for _, e := range snap.Experiments {
		if !keys[e.Flag] {
			continue
		}
		k := keyOf(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// expsForFlag selects the compat-flattened experiments targeting key, in
// fold order (nested first, so the first matching row is the nested
// parent entry when both shapes carry the flag). Callers pass the
// flattenForEval set (EvaluateSnapshot folds it up front); entries carry
// the parent key stamped by Snapshot.UnmarshalJSON / flattenNested.
func expsForFlag(all []releases.SnapshotExperiment, key string) []releases.SnapshotExperiment {
	var out []releases.SnapshotExperiment
	for _, e := range all {
		if e.Flag == key {
			out = append(out, e)
		}
	}
	return out
}

// decodeExperiment converts one stored snapshot experiment to an
// eval.Experiment. The snapshot carries no defaultVariant field, so
// DefaultVariant is "" (Assign falls back to it for empty users or bad
// weights). Undecodable variants -> empty table -> control-path fallthrough.
func decodeExperiment(se releases.SnapshotExperiment) eval.Experiment {
	exp := eval.Experiment{ID: se.ID, Seed: se.Seed}
	if se.Variants == nil {
		return exp
	}
	raw, err := json.Marshal(se.Variants)
	if err != nil {
		return exp
	}
	var variants []eval.Variant
	if err := json.Unmarshal(raw, &variants); err != nil {
		return exp
	}
	exp.Variants = variants
	return exp
}

// rawBytes extracts stored snapshot bytes verbatim from a releases record
// field (covers the PocketBase JSON storage shapes).
func rawBytes(v any) ([]byte, bool) {
	switch t := v.(type) {
	case nil:
		return nil, false
	case string:
		return []byte(t), true
	case json.RawMessage:
		return []byte(t), true
	case []byte:
		return t, true
	case interface{ String() string }:
		return []byte(t.String()), true
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil, false
		}
		return b, true
	}
}

// latestRelease returns the env's max-version release row (nil when the env
// has no release yet). The DB filters by env and returns the top row of a
// version-desc ordering, so cost is one indexed lookup instead of a
// full-table scan; the returned row is the same max-version row as before.
func latestRelease(app core.App, envID string) (*core.Record, error) {
	recs, err := app.FindRecordsByFilter(
		"releases",
		"env = {:env}",
		"-version",
		1, 0,
		dbx.Params{"env": envID},
	)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	return recs[0], nil
}

func setCORS(re *core.RequestEvent) {
	re.Response.Header().Set("Access-Control-Allow-Origin", corsOrigin())
}

// getConfig handles GET /api/v1/env/:env/config.
// Order: CORS stamp -> 401 (key) -> 404 (env) -> 401 (env scope) ->
// 429 (rate) -> 400/414 (attrs) -> 200 empty (no release) -> 304
// (etag match) -> 200. Key+resolve+429 via ingest.RequireKeyEnv.
func getConfig(re *core.RequestEvent) error {
	security.SetHeaders(re)
	setCORS(re)
	_, env, err := ingest.RequireKeyEnv(re, ingest.KindFetch)
	if errors.Is(err, ingest.ErrKeyResponded) {
		return nil // saturation/storage/rate response already written
	}
	if err != nil {
		return err
	}

	anonID, aerr := anonManager.AnonID(env.Id, limits.ClientIP(re), re.Request.UserAgent(), time.Now())
	if aerr != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{"message": "Failed to derive identity.", "status": 500})
	}
	q := re.Request.URL.Query()
	ctx, cerr := BuildContext(q, effectiveUserHash(q, anonID), identity.CountryFromHeadersWith(re.Request.Header, limits.GetConfig().CountryHeaders))
	if cerr != nil {
		if len(re.Request.URL.Query().Get("attrs")) > MaxAttrsBytes {
			return re.JSON(http.StatusRequestURITooLong, map[string]any{"message": cerr.Error(), "status": http.StatusRequestURITooLong})
		}
		return re.BadRequestError(cerr.Error(), nil)
	}

	rel, err := latestRelease(re.App, env.Id)
	if err != nil {
		return err
	}
	if rel == nil {
		setFetchCacheHeaders(re)
		return re.JSON(http.StatusOK, emptyReleaseBody(time.Now().UTC().Format(time.RFC3339Nano)))
	}

	etag := rel.GetString("etag")
	re.Response.Header().Set("ETag", etag)
	if EtagMatches(re.Request.Header.Get("If-None-Match"), etag) {
		setFetchCacheHeaders(re)
		return re.NoContent(http.StatusNotModified)
	}

	snapBytes, ok := rawBytes(rel.GetRaw("snapshot"))
	var snap releases.Snapshot
	if !ok || !json.Valid(snapBytes) || json.Unmarshal(snapBytes, &snap) != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{"message": "Release snapshot is corrupt.", "status": 500})
	}

	values, variants := EvaluateSnapshot(snap, ctx, expOverride(re.Request.URL.Query()))
	setFetchCacheHeaders(re)
	// Legacy-shape snapshots stamp on this 200 config path only: 304/empty
	// paths intentionally do not load the snapshot to stamp (avoids an
	// extra DB read); steady-state cached clients learn of deprecation on
	// next 200. Deprecated: legacy compat goes away in the next server
	// major (republish to migrate).
	if releases.IsLegacyTopLevel(snapBytes) {
		markDeprecatedSnapshot(re.Response.Header())
	}
	// 200-only mint: this runs after the 304 early-return above, so a
	// 304 never mints. BuildContext already 400s malformed ?b=; the error
	// here is unreachable (mint entropy failure -> 500).
	bucket, berr := EffectiveBucket(q)
	if berr != nil {
		if _, perr := ParseBucket(q); perr != nil {
			return re.BadRequestError(berr.Error(), nil)
		}
		return re.JSON(http.StatusInternalServerError, map[string]any{"message": "Failed to mint bucket.", "status": 500})
	}
	return re.JSON(http.StatusOK, evaluatedBody(
		rel.GetInt("version"),
		etag,
		values,
		variants,
		bucket,
		time.Now().UTC().Format(time.RFC3339Nano),
	))
}
