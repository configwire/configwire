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
//   - Context: userHash->UserHash (absent -> anonymous: rules still evaluate,
//     Assign falls back to DefaultVariant), platform/appVersion/locale/
//     country passed through (appVersion=not-semver -> condition false ->
//     defaults fallthrough, still 200, never 400), attrs=<json> parsed as a
//     flat map for custom.* conditions. Canonical ?userHash= wins (16
//     lowercase hex when present); legacy ?uid= is hashed server-side as
//     fallback, flagged deprecated (Task 2 header owns the wire).
//   - attrs error policy: malformed attrs (invalid JSON, or valid JSON that
//     is not an object) -> 400 documented; attrs longer than 8192 bytes ->
//     414 documented. Missing attrs -> empty custom map (anonymous attrs).
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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/ingest"
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

// deprecatedUidFallbackTotal counts process-local ?uid= fallback uses.
var deprecatedUidFallbackTotal atomic.Uint64

// deprecatedSnapshotTotal counts process-local legacy-shape snapshot serves.
var deprecatedSnapshotTotal atomic.Uint64

// markDeprecatedUid stamps the compat header + PII-free log/counter.
// It takes the header (not the request event) so the shared stamping logic
// stays unit-testable; callers pass re.Response.Header(). Never logs
// uid/hash values.
func markDeprecatedUid(h http.Header) {
	h.Set("X-CW-Deprecated-uid", "true")
	deprecatedUidFallbackTotal.Add(1)
	log.Printf("fetch: deprecated ?uid= fallback used (compat window; send userHash)")
}

// markDeprecatedSnapshot stamps the compat header + PII-free log/counter.
// It takes the header (not the request event) so the shared stamping logic
// stays unit-testable; callers pass re.Response.Header(). Never logs
// snapshot contents (counts/shape only).
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
func Register(se *core.ServeEvent) {
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

// isValidUserHash reports whether s matches ^[0-9a-f]{16}$.
func isValidUserHash(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// BuildContext maps fetch query params onto an eval.Context purely (no I/O),
// so it is unit-testable. Precedence: canonical ?userHash= wins when valid
// (16 lowercase hex); legacy ?uid= is hashed server-side via
// eval.HashUserID as fallback when userHash is absent/empty (spaces-trimmed);
// both absent/empty -> anonymous context (empty UserHash/customs).
// It also reports deprecatedUidUsed (true when trimmed ?uid= is non-empty,
// even when canonical userHash wins). An invalid (non-empty, non-16-hex)
// ?userHash= is an error even with ?uid= present.
// platform/appVersion/locale/country pass through untouched (invalid values
// evaluate to false downstream, never 400 here); missing/empty userHash ->
// anonymous context (empty UserHash/customs); malformed userHash, malformed
// attrs, or attrs over MaxAttrsBytes -> error (caller maps to 400/414).
// Missing userHash/attrs -> anonymous context (empty UserHash/customs).
func BuildContext(q url.Values) (eval.Context, bool, error) {
	h := strings.TrimSpace(q.Get("userHash"))
	if h != "" && !isValidUserHash(h) {
		return eval.Context{}, false, fmt.Errorf("invalid userHash: must be 16 lowercase hex.")
	}
	uid := strings.TrimSpace(q.Get("uid"))
	deprecatedUidUsed := uid != ""
	if h == "" && uid != "" {
		h = eval.HashUserID(uid)
	}
	ctx := eval.Context{
		UserHash:    h,
		Platform:    q.Get("platform"),
		AppVersion:  q.Get("appVersion"),
		Locale:      q.Get("locale"),
		Country:     q.Get("country"),
		CustomAttrs: map[string]any{},
	}
	raw := q.Get("attrs")
	if raw == "" {
		return ctx, deprecatedUidUsed, nil
	}
	if len(raw) > MaxAttrsBytes {
		return ctx, false, fmt.Errorf("attrs too large: max %d bytes", MaxAttrsBytes)
	}
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return ctx, false, fmt.Errorf("malformed attrs: not a JSON object")
	}
	if m == nil {
		return ctx, false, fmt.Errorf("malformed attrs: not a JSON object")
	}
	ctx.CustomAttrs = m
	return ctx, deprecatedUidUsed, nil
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

	ctx, deprecatedUid, cerr := BuildContext(re.Request.URL.Query())
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
		if deprecatedUid {
			markDeprecatedUid(re.Response.Header())
		}
		return re.JSON(http.StatusOK, map[string]any{
			"version":  0,
			"etag":     emptyReleaseEtag,
			"values":   map[string]any{},
			"variants": map[string]string{},
			"fetchAt":  time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	etag := rel.GetString("etag")
	re.Response.Header().Set("ETag", etag)
	if EtagMatches(re.Request.Header.Get("If-None-Match"), etag) {
		setFetchCacheHeaders(re)
		if deprecatedUid {
			markDeprecatedUid(re.Response.Header())
		}
		return re.NoContent(http.StatusNotModified)
	}

	snapBytes, ok := rawBytes(rel.GetRaw("snapshot"))
	var snap releases.Snapshot
	if !ok || !json.Valid(snapBytes) || json.Unmarshal(snapBytes, &snap) != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{"message": "Release snapshot is corrupt.", "status": 500})
	}

	values, variants := EvaluateSnapshot(snap, ctx, expOverride(re.Request.URL.Query()))
	setFetchCacheHeaders(re)
	if deprecatedUid {
		markDeprecatedUid(re.Response.Header())
	}
	// Legacy-shape snapshots stamp on this 200 config path only: 304/empty
	// paths intentionally do not load the snapshot to stamp (avoids an
	// extra DB read); steady-state cached clients learn of deprecation on
	// next 200. Unlike uid — which stamps all three success paths — the
	// snapshot stamp hooks here only.
	if releases.IsLegacyTopLevel(snapBytes) {
		markDeprecatedSnapshot(re.Response.Header())
	}
	return re.JSON(http.StatusOK, map[string]any{
		"version":  rel.GetInt("version"),
		"etag":     etag,
		"values":   values,
		"variants": variants,
		"fetchAt":  time.Now().UTC().Format(time.RFC3339Nano),
	})
}
