// Package releases implements the ConfigWire versioning heart.
//
// FROZEN SNAPSHOT SCHEMA (nested experiments shape):
//
//		{"flags":[{"key","type","default","group","rules":[{"priority","condition","value"}],
//		 "experiments":[{"id","seed","variants","status"}]}]}
//
//	  - flags/rules/experiments are scoped to the publish target env's project
//	    (env.project relation). flags sorted by key, rules by priority
//	    ascending, nested experiments by seed (id tiebreak) so the canonical
//	    bytes (and therefore the etag) are deterministic.
//	  - experiments are scoped to the publish target env's project via
//	    their linked flag: rows targeting a flag of another project are
//	    EXCLUDED (no cross-project leakage into snapshots, and therefore
//	    into fetch overlays, stats, or ingest). Untargeted rows (flag
//	    relation unset or dangling) are EXCLUDED (breaking change: the
//	    legacy flag "" shape is removed). The parent flag key is implicit on
//	    the wire (no per-experiment flag JSON field; the Go struct keeps an
//	    in-memory-only stamped copy for eval, json:"-"). experiment.id is
//	    the record id.
//	  - flag.group is the linked groups record id ("" when unset).
//	  - READ COMPAT: rows stored under the legacy shape
//	    {"flags":[...],"experiments":[{"id","flag","seed",...}]} still
//	    unmarshal via Snapshot.UnmarshalJSON, which folds legacy top-level
//	    experiments into their owning flag by flag key and drops orphans
//	    (unknown flag key). Legacy duplicates already present nested are
//	    skipped (dual-emit fold dedupes by id, flag+seed fallback) so all
//	    three stored shapes (legacy-only, nested-only, dual) fold to
//	    identical in-memory sets.
//	  - WRITE COMPAT (dual-emit): MarshalCanonical emits BOTH the nested
//	    per-flag experiments AND a derived legacy top-level experiments[]
//	    duplicate (same rows, parent flag key stamped, sorted by
//	    flag+seed+id) so old out-of-band direct-snapshot parsers keep
//	    working after republish. The top-level array is always present
//	    ([] when no experiments) and never null.
//
// ETAG RULE (T10/T13/T17 contract):
//
//	etag = hex(sha256(version + ":" + snapshot))[:16]
//
// where snapshot is the canonical server-marshaled snapshot bytes
// (encoding/json over the structs below). The version is part of the
// hash input, so a rollback row carries a FRESH etag even though its
// snapshot bytes are byte-identical to the source row.
//
// 409 SEMANTICS: publish carries {note, baseVersion}; baseVersion must
// equal the env's current max version (0 when no release exists yet).
// Mismatch -> 409 {"currentVersion":N} with NO write.
//
// SERVER-SIDE BUILD RATIONALE: the snapshot is assembled from the live
// flags/rules/experiments collections inside the handler, never accepted
// from the client. This reuses the key-shape/cap/type hooks on a dry
// assemble (validation runs over live rows; no writes happen except the
// single releases row), so a malicious or stale client cannot ship a
// snapshot that the collections themselves would reject.
//
// AUDIT: structured server log line per publish/rollback
// (who/version/etag/note). No new collection — events kinds are
// fetch|exposure only.
package releases

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/flagkey"

	"github.com/pocketbase/dbx"

	"github.com/pocketbase/pocketbase/core"
)

// Flag key shape is enforced by flagkey.Check so the dry assemble
// rejects exactly what the write path would reject.

// Flag types accepted in a snapshot (mirrors the flags.type select).
var validFlagTypes = map[string]bool{
	eval.TypeNumber: true,
	eval.TypeString: true,
	eval.TypeBool:   true,
	eval.TypeJSON:   true,
}

// SnapshotRule is one prioritized conditional value inside a snapshot flag.
type SnapshotRule struct {
	Priority  int `json:"priority"`
	Condition any `json:"condition"`
	Value     any `json:"value"`
}

// SnapshotFlag is one flag entry in the frozen publish payload.
type SnapshotFlag struct {
	Key         string               `json:"key"`
	Type        string               `json:"type"`
	Default     any                  `json:"default"`
	Group       string               `json:"group"`
	Rules       []SnapshotRule       `json:"rules"`
	Experiments []SnapshotExperiment `json:"experiments"`
}

// MarshalJSON guarantees Rules/Experiments encode as [] (never null).
func (f SnapshotFlag) MarshalJSON() ([]byte, error) {
	type plain SnapshotFlag
	c := f
	if c.Rules == nil {
		c.Rules = []SnapshotRule{}
	}
	if c.Experiments == nil {
		c.Experiments = []SnapshotExperiment{}
	}
	return json.Marshal(plain(c))
}

// SnapshotExperiment is one experiment nested under its parent flag in the
// frozen publish payload. The parent flag key is implicit on the wire (no
// "flag" JSON field); Flag carries it in memory only (json:"-") so fetch
// eval and the handler fold keep working without a JSON round-trip.
type SnapshotExperiment struct {
	ID       string `json:"id"`
	Flag     string `json:"-"`
	Seed     string `json:"seed"`
	Variants any    `json:"variants"`
	Status   string `json:"status"`
}

// Snapshot is the FROZEN publish payload stored on releases.snapshot.
// Experiments is the in-memory flattened eval-ready set (every nested
// entry with Flag stamped, flag order, orphans dropped); never marshaled
// (json:"-") — the wire shape is dual-emit (nested plus derived
// top-level). Populated by BuildSnapshot and UnmarshalJSON; prefer
// per-flag Experiments for new code.
// Experiments must only ever come from flattenNested via UnmarshalJSON /
// BuildSnapshot and never be set literally, since ValidateSnapshot only
// checks per-flag nested entries.
type Snapshot struct {
	Flags       []SnapshotFlag       `json:"flags"`
	Experiments []SnapshotExperiment `json:"-"`
}

// MarshalJSON guarantees Flags encodes as [] (never null); nested nil
// slices are handled by SnapshotFlag.MarshalJSON.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	type plain Snapshot
	c := s
	if c.Flags == nil {
		c.Flags = []SnapshotFlag{}
	}
	return json.Marshal(plain(c))
}

// snapshotCompatExperiment is one legacy top-level experiment row — the
// linked flag's KEY plus the experiment fields. Read path: parses rows
// stored under the pre-nesting shape; write path: MarshalCanonical derives
// the same rows from the nested per-flag set (dual-emit).
type snapshotCompatExperiment struct {
	ID       string `json:"id"`
	Flag     string `json:"flag"`
	Seed     string `json:"seed"`
	Variants any    `json:"variants"`
	Status   string `json:"status"`
}

// Deprecated (kept for back-compat): the legacy stored shape
// ({"flags":[...],"experiments":[...]}). The top-level experiments array is
// optional; when present its entries fold into their owning flag.
type snapshotCompat struct {
	Flags       []SnapshotFlag             `json:"flags"`
	Experiments []snapshotCompatExperiment `json:"experiments"`
}

// IsLegacyTopLevel reports whether raw is a legacy-only snapshot: a
// non-empty top-level experiments array with NO nested per-flag
// experiments. Dual-emitted rows (nested plus derived top-level, the
// current MarshalCanonical shape) report false so republished snapshots
// do not stamp the deprecated-snapshot header; only unmigrated
// legacy-only rows stamp. Missing, empty, nested-only, or malformed
// payloads report false. Pure: no I/O, no logging, payload contents never
// retained.
//
// Deprecated: legacy top-level shape is deprecated and will be rejected in
// the next server major; republish to migrate.
//
// Detector choice (a): gate on nested presence rather than an explicit
// version marker. A marker would require a schema bump every reader must
// understand; presence-gating keeps old parsers (which ignore unknown
// fields anyway) working and needs no migration.
func IsLegacyTopLevel(raw []byte) bool {
	var probe struct {
		Flags []struct {
			Experiments json.RawMessage `json:"experiments"`
		} `json:"flags"`
		Experiments json.RawMessage `json:"experiments"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if len(probe.Experiments) == 0 {
		return false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(probe.Experiments, &arr); err != nil {
		return false
	}
	if len(arr) == 0 {
		return false
	}
	for _, f := range probe.Flags {
		if len(f.Experiments) == 0 {
			continue
		}
		var nested []json.RawMessage
		if err := json.Unmarshal(f.Experiments, &nested); err != nil {
			continue
		}
		if len(nested) > 0 {
			return false
		}
	}
	return true
}

// UnmarshalJSON reads all three stored shapes (legacy-only, nested-only,
// dual-emit): legacy top-level experiments fold into their owning flag by
// flag key; orphans (unknown flag key) are dropped; rows already present
// nested (same id, or same flag+seed when id-less) are skipped so dual
// bytes fold to the identical in-memory set as nested-only. Nested
// experiments sort by seed (id tiebreak) so post-fold order is deterministic.
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var compat snapshotCompat
	if err := json.Unmarshal(data, &compat); err != nil {
		return err
	}
	flags := compat.Flags
	if flags == nil {
		flags = []SnapshotFlag{}
	}
	byKey := make(map[string]*SnapshotFlag, len(flags))
	for i := range flags {
		if flags[i].Rules == nil {
			flags[i].Rules = []SnapshotRule{}
		}
		if flags[i].Experiments == nil {
			flags[i].Experiments = []SnapshotExperiment{}
		}
		if _, exists := byKey[flags[i].Key]; !exists {
			byKey[flags[i].Key] = &flags[i]
		}
	}
	seen := make(map[string]bool)
	for _, f := range flags {
		for _, e := range f.Experiments {
			seen[dedupeKey(f.Key, e.ID, e.Seed)] = true
		}
	}
	// Deprecated (kept for back-compat): fold legacy top-level experiments
	// into their owning flag by flag key; orphans (unknown flag key) drop.
	for _, e := range compat.Experiments {
		f, ok := byKey[e.Flag]
		if !ok {
			continue
		}
		k := dedupeKey(e.Flag, e.ID, e.Seed)
		if seen[k] {
			continue
		}
		seen[k] = true
		f.Experiments = append(f.Experiments, SnapshotExperiment{
			ID:       e.ID,
			Seed:     e.Seed,
			Variants: e.Variants,
			Status:   e.Status,
		})
	}
	for i := range flags {
		sort.SliceStable(flags[i].Experiments, func(a, b int) bool {
			if flags[i].Experiments[a].Seed != flags[i].Experiments[b].Seed {
				return flags[i].Experiments[a].Seed < flags[i].Experiments[b].Seed
			}
			return flags[i].Experiments[a].ID < flags[i].Experiments[b].ID
		})
	}
	s.Flags = flags
	s.Experiments = flattenNested(flags)
	return nil
}

// dedupeKey identifies one experiment row for the dual-fold skip and
// mirrors fetch.flattenForEval: record ids are globally unique so id wins;
// id-less rows fall back to parent flag + seed.
func dedupeKey(flag, id, seed string) string {
	if id != "" {
		return "id\x00" + id
	}
	return "seed\x00" + flag + "\x00" + seed
}

// flattenNested returns the in-memory eval-ready set: every nested entry
// with Flag stamped, in flag order (nested per-flag order preserved).
// Orphans cannot occur here (fold drops them); the result feeds fetch eval
// and the handler fold without a JSON round-trip.
func flattenNested(flags []SnapshotFlag) []SnapshotExperiment {
	out := []SnapshotExperiment{}
	for i := range flags {
		for _, e := range flags[i].Experiments {
			c := e
			c.Flag = flags[i].Key
			out = append(out, c)
		}
	}
	return out
}

// EtagFor bumps the etag on every version bump even when snapshot bytes are identical (rollback requirement).
func EtagFor(version int, snapshot []byte) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", version, snapshot)))
	return hex.EncodeToString(sum[:])[:16]
}

// NextVersion returns the next version after currentMax (max+1, starting at 1).
func NextVersion(currentMax int) int {
	return currentMax + 1
}

// CheckBaseVersion mismatch -> caller returns 409 with currentVersion and performs NO write.
func CheckBaseVersion(baseVersion, currentMax int) bool {
	return baseVersion == currentMax
}

// MarshalCanonical renders canonical bytes; the etag is computed over exactly these bytes.
// Dual-emit: nested per-flag experiments plus a derived legacy top-level
// experiments[] duplicate (same rows, parent flag key stamped) sorted by
// flag+seed+id so old out-of-band readers keep parsing after republish.
// The top-level array is always emitted ([] when empty, never null).
func MarshalCanonical(snap Snapshot) ([]byte, error) {
	if snap.Flags == nil {
		snap.Flags = []SnapshotFlag{}
	}
	for i := range snap.Flags {
		if snap.Flags[i].Rules == nil {
			snap.Flags[i].Rules = []SnapshotRule{}
		}
		if snap.Flags[i].Experiments == nil {
			snap.Flags[i].Experiments = []SnapshotExperiment{}
		}
	}
	legacy := []snapshotCompatExperiment{}
	for _, f := range snap.Flags {
		for _, e := range f.Experiments {
			legacy = append(legacy, snapshotCompatExperiment{
				ID:       e.ID,
				Flag:     f.Key,
				Seed:     e.Seed,
				Variants: e.Variants,
				Status:   e.Status,
			})
		}
	}
	sort.SliceStable(legacy, func(i, j int) bool {
		if legacy[i].Flag != legacy[j].Flag {
			return legacy[i].Flag < legacy[j].Flag
		}
		if legacy[i].Seed != legacy[j].Seed {
			return legacy[i].Seed < legacy[j].Seed
		}
		return legacy[i].ID < legacy[j].ID
	})
	wire := struct {
		Flags       []SnapshotFlag             `json:"flags"`
		Experiments []snapshotCompatExperiment `json:"experiments"`
	}{
		Flags:       snap.Flags,
		Experiments: legacy,
	}
	return json.Marshal(wire)
}

func jsonAny(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case json.RawMessage:
		var out any
		if err := json.Unmarshal(t, &out); err != nil {
			return string(t)
		}
		return out
	case string:
		var out any
		if err := json.Unmarshal([]byte(t), &out); err != nil {
			return t
		}
		return out
	case []byte:
		var out any
		if err := json.Unmarshal(t, &out); err != nil {
			return string(t)
		}
		return out
	default:
		// PocketBase JSON fields surface as types.JSONRaw which is a
		// []byte kind underneath but a distinct named type; handle it
		// via a marshal round-trip.
		raw, err := json.Marshal(t)
		if err != nil {
			return t
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return t
		}
		return out
	}
}

// BuildSnapshot assembles the FROZEN snapshot for env from the live
// collections (server-side only — never from client input). Flags,
// rules, and experiments are scoped to env's project; experiments
// targeting another project's flags are excluded (no cross-project
// leakage). Returned flags are sorted by key, rules by priority
// ascending, nested experiments by seed (id tiebreak).
func BuildSnapshot(app core.App, env *core.Record) (Snapshot, error) {
	snap := Snapshot{Flags: []SnapshotFlag{}}
	projectID := env.GetString("project")

	flagRecs, err := app.FindRecordsByFilter("flags", "project = {:project}", "", 0, 0, dbx.Params{"project": projectID})
	if err != nil {
		return snap, err
	}
	byID := map[string]*core.Record{}
	flagKeys := map[string]string{}
	flagIDs := make([]string, 0, len(flagRecs))
	for _, fr := range flagRecs {
		byID[fr.Id] = fr
		flagKeys[fr.Id] = fr.GetString("key")
		flagIDs = append(flagIDs, fr.Id)
	}

	ruleRecs, err := findFlagScopedRecords(app, "rules", flagIDs)
	if err != nil {
		return snap, err
	}
	rulesByFlag := map[string][]SnapshotRule{}
	for _, rr := range ruleRecs {
		flagID := rr.GetString("flag")
		if _, ok := byID[flagID]; !ok {
			continue
		}
		rulesByFlag[flagID] = append(rulesByFlag[flagID], SnapshotRule{
			Priority:  rr.GetInt("priority"),
			Condition: jsonAny(rr.Get("condition")),
			Value:     jsonAny(rr.Get("value")),
		})
	}

	expRecs, err := findFlagScopedRecords(app, "experiments", flagIDs)
	if err != nil {
		return snap, err
	}
	flagProjects := make(map[string]string, len(flagRecs))
	for _, fr := range flagRecs {
		flagProjects[fr.Id] = fr.GetString("project")
	}
	expsByFlag := make(map[string][]SnapshotExperiment)
	for _, er := range expRecs {
		flagKey, include := ResolveExperimentFlag(projectID, er.GetString("flag"), flagProjects, flagKeys)
		if !include {
			continue
		}
		expsByFlag[flagKey] = append(expsByFlag[flagKey], SnapshotExperiment{
			ID:       er.Id,
			Seed:     er.GetString("seed"),
			Variants: jsonAny(er.Get("variants")),
			Status:   er.GetString("status"),
		})
	}

	for id, fr := range byID {
		rules := rulesByFlag[id]
		if rules == nil {
			rules = []SnapshotRule{}
		}
		sort.SliceStable(rules, func(i, j int) bool {
			return rules[i].Priority < rules[j].Priority
		})
		exps := expsByFlag[flagKeys[id]]
		if exps == nil {
			exps = []SnapshotExperiment{}
		}
		sort.SliceStable(exps, func(i, j int) bool {
			if exps[i].Seed != exps[j].Seed {
				return exps[i].Seed < exps[j].Seed
			}
			return exps[i].ID < exps[j].ID
		})
		snap.Flags = append(snap.Flags, SnapshotFlag{
			Key:         flagKeys[id],
			Type:        fr.GetString("type"),
			Default:     jsonAny(fr.Get("defaultValue")),
			Group:       fr.GetString("group"),
			Rules:       rules,
			Experiments: exps,
		})
	}
	sort.SliceStable(snap.Flags, func(i, j int) bool {
		return snap.Flags[i].Key < snap.Flags[j].Key
	})
	snap.Experiments = flattenNested(snap.Flags)

	return snap, nil
}

// findFlagScopedRecords returns every row of collection whose single-select
// `flag` relation (MaxSelect 1, plain id column) is one of flagIDs, via one
// indexed IN query. FindRecordsByFilter cannot express this: its params are
// stringified before parsing, so a []string would become a single JSON-array
// string literal and silently match zero rows.
func findFlagScopedRecords(app core.App, collection string, flagIDs []string) ([]*core.Record, error) {
	if len(flagIDs) == 0 {
		return []*core.Record{}, nil
	}
	args := make([]any, len(flagIDs))
	for i, id := range flagIDs {
		args[i] = id
	}
	recs := []*core.Record{}
	if err := app.RecordQuery(collection).AndWhere(dbx.In("flag", args...)).All(&recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// ResolveExperimentFlag decides whether one experiment row belongs in
// the snapshot for projectID, purely (no I/O) so scoping is
// unit-testable. projects/keys index every flag by record id (built
// once per BuildSnapshot from the already-loaded flags scan, so no
// extra queries). Untargeted rows (flagID "") or dangling relations
// are EXCLUDED (breaking change: legacy flag "" shape removed); rows
// another project are excluded — that exclusion is what keeps one
// project's experiments out of another project's snapshots.
func ResolveExperimentFlag(projectID, flagID string, projects, keys map[string]string) (key string, include bool) {
	if flagID == "" {
		return "", false
	}
	p, ok := projects[flagID]
	if !ok {
		return "", false
	}
	if p != projectID {
		return "", false
	}
	return keys[flagID], true
}

// coerceToType mirrors eval's rule-value coercion (eval.coerce is private):
// numbers accept any Go numeric, strings/bools require their exact type,
// json accepts objects (maps) and arrays (slices) only.
func coerceToType(v any, t string) bool {
	switch t {
	case eval.TypeNumber:
		return isNumber(v)
	case eval.TypeString:
		_, ok := v.(string)
		return ok
	case eval.TypeBool:
		_, ok := v.(bool)
		return ok
	case eval.TypeJSON:
		return isJSONObject(v) || isJSONArray(v)
	default:
		return false
	}
}

func isNumber(v any) bool {
	switch v.(type) {
	case float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, json.Number:
		return true
	default:
		return false
	}
}

func isJSONObject(v any) bool {
	if v == nil {
		return false
	}
	if _, ok := v.(map[string]any); ok {
		return true
	}
	return false
}

func isJSONArray(v any) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case []any, []map[string]any:
		return true
	case string, []byte:
		return false
	default:
		return false
	}
}

// Condition op allowlists mirror configwire/eval/eval.go EXACTLY (the frozen
// contract header + the matchCondition switch): adding an op to eval
// requires adding it here, or publish keeps rejecting it.
var (
	stringFieldOps  = map[string]bool{"==": true, "!=": true, "contains": true, "regex": true}
	versionFieldOps = map[string]bool{
		"<": true, "<=": true, "==": true, "!=": true,
		">=": true, ">": true, "contains": true, "regex": true,
	}
	percentileOps  = map[string]bool{"<=": true, "between": true}
	customFieldOps = map[string]bool{
		"==": true, "!=": true, "<": true, "<=": true,
		">": true, ">=": true, "contains": true, "regex": true,
	}
)

// maxConditionsPerRule bounds the array condition form so one rule cannot
// force unbounded eval cost (ruleMatches ANDs every entry).
const maxConditionsPerRule = 10

// validateSingleCondition checks one {field,op,value[,seed]} object with the
// frozen field/op allowlists. m is the already-decoded object shape.
func validateSingleCondition(flagKey string, ruleIndex int, m map[string]json.RawMessage) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("flag %q: rule %d has invalid condition: "+format, append([]any{flagKey, ruleIndex}, args...)...)
	}
	var field, op string
	if f, ok := m["field"]; !ok || json.Unmarshal(f, &field) != nil || field == "" {
		return bad("field must be a non-empty string")
	}
	if o, ok := m["op"]; !ok || json.Unmarshal(o, &op) != nil || op == "" {
		return bad("op must be a non-empty string")
	}
	if _, ok := m["value"]; !ok {
		return bad("missing value")
	}
	var allowed map[string]bool
	switch {
	case field == "platform" || field == "locale" || field == "country":
		allowed = stringFieldOps
	case field == "appVersion":
		allowed = versionFieldOps
	case field == "percentile":
		allowed = percentileOps
	case strings.HasPrefix(field, "custom."):
		if len(field) == len("custom.") {
			return bad("custom attribute name must not be empty")
		}
		allowed = customFieldOps
	default:
		return bad("unknown field %q", field)
	}
	if !allowed[op] {
		return bad("op %q not allowed for field %q", op, field)
	}
	if op == "regex" {
		var value string
		if err := json.Unmarshal(m["value"], &value); err == nil {
			if err := eval.ValidateRegexPattern(value); err != nil {
				return bad("invalid regex: %v", err)
			}
		}
	}
	return nil
}

// validateCondition accepts either a single {field,op,value[,seed]} object
// (backward compat) or a non-empty array of such objects (AND semantics
// downstream in eval.ruleMatches). It rejects rule conditions that eval
// would silently treat as false (unknown field/op, non-object shape,
// missing value key, empty array, non-object array elements).
// Without this, a corrupt condition decodes to zero conditions downstream
// and vacuously MATCHES — fail-open. Publish-time rejection keeps corrupt
// conditions (single or inside arrays) fail-closed: they never reach a
// snapshot. The value key must EXIST (any JSON including null counts);
// a missing value key is rejected.
func validateCondition(flagKey string, ruleIndex int, cond any) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("flag %q: rule %d has invalid condition: "+format, append([]any{flagKey, ruleIndex}, args...)...)
	}
	if cond == nil {
		return bad("must be a JSON object with field/op/value")
	}
	raw, err := json.Marshal(cond)
	if err != nil {
		return bad("must be a JSON object with field/op/value")
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return bad("must be a JSON object with field/op/value")
		}
		if len(arr) == 0 {
			return bad("condition array must not be empty")
		}
		if len(arr) > maxConditionsPerRule {
			return bad("condition array must have at most %d entries", maxConditionsPerRule)
		}
		for i, elem := range arr {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(elem, &m); err != nil || m == nil {
				return bad("condition index %d must be a JSON object with field/op/value", i)
			}
			if err := validateSingleCondition(flagKey, ruleIndex, m); err != nil {
				return err
			}
		}
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return bad("must be a JSON object with field/op/value")
	}
	return validateSingleCondition(flagKey, ruleIndex, m)
}

// ValidateSnapshot runs the publish dry-assemble checks (no writes).
func ValidateSnapshot(snap Snapshot) error {
	if len(snap.Flags) == 0 {
		return errors.New("nothing to publish: no flags in this env's project")
	}
	for _, f := range snap.Flags {
		if err := flagkey.Check(f.Key); err != nil {
			return fmt.Errorf("invalid flag key %q: must match ^[A-Za-z_][A-Za-z0-9_.-]*$ and be 1-%d chars", f.Key, flagkey.MaxLen)
		}
		if !validFlagTypes[f.Type] {
			return fmt.Errorf("invalid flag type %q for flag %q: must be number|string|bool|json", f.Type, f.Key)
		}
		if f.Default != nil && !coerceToType(f.Default, f.Type) {
			return fmt.Errorf("flag %q: default value does not match type %q", f.Key, f.Type)
		}
		for i, r := range f.Rules {
			if r.Value != nil && !coerceToType(r.Value, f.Type) {
				return fmt.Errorf("flag %q: rule value does not match type %q", f.Key, f.Type)
			}
			if err := validateCondition(f.Key, i, r.Condition); err != nil {
				return err
			}
		}
		for _, e := range f.Experiments {
			raw, err := json.Marshal(e.Variants)
			if err != nil {
				return fmt.Errorf("flag %q: experiment %q: variants are not valid JSON", f.Key, e.ID)
			}
			var variants []eval.Variant
			if err := json.Unmarshal(raw, &variants); err != nil {
				return fmt.Errorf("flag %q: experiment %q: variants shape invalid: %v", f.Key, e.ID, err)
			}
			if err := eval.ValidateExperiment(eval.Experiment{Variants: variants}); err != nil {
				return fmt.Errorf("flag %q: experiment %q: %v", f.Key, e.ID, err)
			}
		}
	}
	return nil
}

// MaxVersionForEnv returns the env's current max release version, or 0
// when none exists, via one indexed lookup (env filter + version-desc
// top row) instead of a full-table scan.
func MaxVersionForEnv(app core.App, envID string) (int, error) {
	recs, err := app.FindRecordsByFilter("releases", "env = {:env}", "-version", 1, 0, dbx.Params{"env": envID})
	if err != nil {
		return 0, err
	}
	if len(recs) == 0 {
		return 0, nil
	}
	return recs[0].GetInt("version"), nil
}
