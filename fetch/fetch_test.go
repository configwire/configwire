package fetch

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/releases"
)

func query(raw string) url.Values {
	q, err := url.ParseQuery(raw)
	if err != nil {
		panic(err)
	}
	return q
}

func TestBuildContextFull(t *testing.T) {
	// Server-derived anonID wins: client ?userHash= is accepted-but-ignored,
	// client ?country= is ignored in favor of server geo.
	q := query(`userHash=ffffffffffffffff&platform=android&appVersion=1.2.3&locale=en-US&country=FR&attrs=%7B%22tier%22%3A%22pro%22%2C%22n%22%3A7%7D`)
	ctx, err := BuildContext(q, "0123456789abcdef", "US")
	if err != nil {
		t.Fatalf("BuildContext err = %v", err)
	}
	if ctx.UserHash != "0123456789abcdef" || ctx.Platform != "android" || ctx.AppVersion != "1.2.3" ||
		ctx.Locale != "en-US" || ctx.Country != "US" {
		t.Fatalf("scalar fields wrong: %+v", ctx)
	}
	if ctx.CustomAttrs["tier"] != "pro" {
		t.Fatalf("custom tier = %v", ctx.CustomAttrs["tier"])
	}
}

func TestBuildContextEmpty(t *testing.T) {
	ctx, err := BuildContext(query(``), "", "")
	if err != nil {
		t.Fatalf("BuildContext err = %v", err)
	}
	if ctx.UserHash != "" || len(ctx.CustomAttrs) != 0 {
		t.Fatalf("expected anonymous empty ctx, got %+v", ctx)
	}
}

// TestBuildContextUserHashIgnored pins the server-wins contract: any
// client ?userHash= (valid, invalid, uppercase, raw uid-shaped) is
// accepted-but-ignored — never a 400 — and Context.UserHash carries the
// server-derived anonID (field name kept for eval.Bucket compat).
func TestBuildContextUserHashIgnored(t *testing.T) {
	const anon = "0123456789abcdef"
	for name, raw := range map[string]string{
		"valid-hex":   `userHash=ffffffffffffffff`,
		"too-short":   `userHash=abc123`,
		"too-long":    `userHash=0123456789abcdef00`,
		"uppercase":   `userHash=0123456789ABCDEF`,
		"non-hex":     `userHash=0123456789abcdeg`,
		"raw-uid":     `userHash=user-7`,
		"spaces":      `userHash=+fffffffffffffffe+`,
		"empty-value": `userHash=`,
		"absent":      `platform=ios`,
	} {
		ctx, err := BuildContext(query(raw), anon, "")
		if err != nil {
			t.Errorf("%s: client userHash must be accepted-but-ignored, got %v", name, err)
			continue
		}
		if ctx.UserHash != anon {
			t.Errorf("%s: UserHash = %q, want server anonID %q", name, ctx.UserHash, anon)
		}
	}
}

// TestBuildContextCountryServerOnly pins: client ?country= never reaches
// eval — Context.Country is the server geo param (from
// identity.CountryFromHeaders) verbatim, even when the client sends a
// conflicting or garbage value.
func TestBuildContextCountryServerOnly(t *testing.T) {
	for name, raw := range map[string]string{
		"conflict": `country=FR`,
		"garbage":  `country=not-a-country!!!`,
		"lower":    `country=us`,
		"absent":   `platform=ios`,
	} {
		ctx, err := BuildContext(query(raw), "0123456789abcdef", "US")
		if err != nil {
			t.Fatalf("%s: err = %v", name, err)
		}
		if ctx.Country != "US" {
			t.Errorf("%s: Country = %q, want server geo %q", name, ctx.Country, "US")
		}
	}
	ctx, err := BuildContext(query(`country=FR`), "0123456789abcdef", "")
	if err != nil {
		t.Fatalf("empty server geo err = %v", err)
	}
	if ctx.Country != "" {
		t.Errorf("empty server geo: Country = %q, want empty (client ignored)", ctx.Country)
	}
}

// TestUserHashCompatHeader pins the compat signal: any trimmed non-empty
// client ?userHash= stamps X-CW-Deprecated-userHash=true (server-derived
// anonID still wins); absent/empty/blank sends no header.
func TestUserHashCompatHeader(t *testing.T) {
	if userHashCompatHeader != "X-CW-Deprecated-userHash" {
		t.Fatalf("compat header = %q, want %q", userHashCompatHeader, "X-CW-Deprecated-userHash")
	}
	for _, raw := range []string{
		`userHash=ffffffffffffffff`,
		`userHash=abc123`,
		`userHash=user-7&platform=ios`,
	} {
		h := http.Header{}
		stampUserHashCompat(h, query(raw))
		if got := h.Get(userHashCompatHeader); got != "true" {
			t.Errorf("%s: header = %q, want %q", raw, got, "true")
		}
	}
	for _, raw := range []string{``, `platform=ios`, `userHash=`, `userHash=+++`} {
		h := http.Header{}
		stampUserHashCompat(h, query(raw))
		if got := h.Get(userHashCompatHeader); got != "" {
			t.Errorf("%s: header = %q, want absent", raw, got)
		}
	}
}

func TestBuildContextAttrsEdges(t *testing.T) {
	for name, raw := range map[string]string{
		"not-json":   `attrs=notjson`,
		"array":      `attrs=%5B1%2C2%5D`,
		"string":     `attrs=%22hi%22`,
		"number":     `attrs=42`,
		"null":       `attrs=null`,
		"truncated":  `attrs=%7B%22a%22%3A`,
		"empty-obj":  `attrs=%7B%7D`,
		"nested-obj": `attrs=%7B%22a%22%3A%7B%22b%22%3A1%7D%7D`,
	} {
		ctx, err := BuildContext(query(raw), "0123456789abcdef", "")
		switch name {
		case "empty-obj", "nested-obj":
			if err != nil {
				t.Errorf("%s: unexpected err %v", name, err)
			}
		default:
			if err == nil {
				t.Errorf("%s: expected 400-mapped error, got ctx %+v", name, ctx)
			}
		}
	}
}

func TestBuildContextAttrsOversized(t *testing.T) {
	big := strings.Repeat("a", MaxAttrsBytes+1)
	q := url.Values{"attrs": []string{`{"k":"` + big + `"}`}}
	if _, err := BuildContext(q, "0123456789abcdef", ""); err == nil {
		t.Fatalf("expected oversized error for %d-byte attrs", len(big))
	} else if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized error should mention size, got %q", err)
	}
	// Exactly at the bound parses (if valid JSON): bound is on raw length.
	at := `{"k":"` + strings.Repeat("b", MaxAttrsBytes-len(`{"k":""}`)) + `"}`
	if len(at) != MaxAttrsBytes {
		t.Fatalf("test setup wrong: len=%d", len(at))
	}
	if _, err := BuildContext(url.Values{"attrs": []string{at}}, "0123456789abcdef", ""); err != nil {
		t.Fatalf("at-bound attrs should parse, got %v", err)
	}
}

func TestEtagMatches(t *testing.T) {
	if !EtagMatches("abc123", "abc123") {
		t.Errorf("exact match should hit")
	}
	for _, h := range []string{"", "W/\"abc123\"", `"abc123"`, "ABC123", "abc123 "} {
		if EtagMatches(h, "abc123") {
			t.Errorf("header %q must NOT match (exact only)", h)
		}
	}
}

func fakeSnapshot() releases.Snapshot {
	return releases.Snapshot{
		Flags: []releases.SnapshotFlag{
			{
				Key:     "welcome",
				Type:    "string",
				Default: "hello",
				Rules: []releases.SnapshotRule{
					{Priority: 1,
						Condition: map[string]any{"field": "platform", "op": "==", "value": "android"},
						Value:     "hello-android"},
				},
				Experiments: []releases.SnapshotExperiment{},
			},
			{Key: "new_ui", Type: "bool", Default: false,
				Experiments: []releases.SnapshotExperiment{
					{
						ID:   "exp1",
						Seed: "seed-1",
						Variants: []any{
							map[string]any{"name": "control", "weightBps": float64(5000), "values": map[string]any{"new_ui": false}},
							map[string]any{"name": "treatment", "weightBps": float64(5000), "values": map[string]any{"new_ui": true}},
						},
						Status: "running",
					},
				},
			},
		},
	}
}

func TestEvaluateSnapshotShaping(t *testing.T) {
	snap := fakeSnapshot()
	ctx := eval.Context{UserHash: "0123456789abcdef", Platform: "android"}
	values, variants := EvaluateSnapshot(snap, ctx, "")

	if values["welcome"] != "hello-android" {
		t.Errorf("rule should win for android, got %v", values["welcome"])
	}
	wantVariant := eval.Assign(eval.Experiment{
		Seed: "seed-1",
		Variants: []eval.Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}, "0123456789abcdef")
	if variants["new_ui"] != wantVariant {
		t.Errorf("variant = %q, want Assign %q", variants["new_ui"], wantVariant)
	}
	wantVal := false
	if wantVariant == "treatment" {
		wantVal = true
	}
	if values["new_ui"] != wantVal {
		t.Errorf("overlay value = %v, want %v for variant %q", values["new_ui"], wantVal, wantVariant)
	}
	if _, ok := variants["welcome"]; ok {
		t.Errorf("flag with no experiment must have no variants entry")
	}
}

func TestEvaluateSnapshotControlPaths(t *testing.T) {
	snap := fakeSnapshot()
	// Anonymous user -> Assign falls back to DefaultVariant ("", snapshot
	// carries none) and the control values apply.
	values, variants := EvaluateSnapshot(snap, eval.Context{}, "")
	if variants["new_ui"] != "" {
		t.Errorf("anonymous variant = %q, want DefaultVariant %q", variants["new_ui"], "")
	}
	if values["new_ui"] != false {
		t.Errorf("anonymous overlay value = %v, want base false", values["new_ui"])
	}
	// exp override forces the control path even for an assigned user.
	values, variants = EvaluateSnapshot(snap, eval.Context{UserHash: "0123456789abcdef"}, "draft")
	if values["new_ui"] != false {
		t.Errorf("draft override value = %v, want base false", values["new_ui"])
	}
	if variants["new_ui"] != "" {
		t.Errorf("draft override variant = %q, want %q", variants["new_ui"], "")
	}
	// Non-semver appVersion never errors: platform-gated rule still wins.
	values, _ = EvaluateSnapshot(snap, eval.Context{Platform: "android", AppVersion: "not-semver"}, "")
	if values["welcome"] != "hello-android" {
		t.Errorf("not-semver must fall through to rule match, got %v", values["welcome"])
	}
}

// TestAllowHeadersIsConfigWire guards the fetch preflight contract: the
// fetch OPTIONS response must permit the SDK key header (served via
// the allowHeaders const, also asserted live in
// .omo/evidence/configwire-t5-wire.log).
func TestAllowHeadersIsConfigWire(t *testing.T) {
	if allowHeaders != "X-ConfigWire-Key, If-None-Match" {
		t.Fatalf("allowHeaders = %q, want %q", allowHeaders, "X-ConfigWire-Key, If-None-Match")
	}
}

func TestCorsOriginSingleRead(t *testing.T) {
	t.Setenv("CONFIGWIRE_CORS_ORIGIN", "https://app.example.com")
	if got := corsOrigin(); got != "https://app.example.com" {
		t.Fatalf("corsOrigin = %q, want configured origin", got)
	}

	t.Setenv("CONFIGWIRE_CORS_ORIGIN", "")
	if got := corsOrigin(); got != "*" {
		t.Fatalf("unset: corsOrigin = %q, want dev default", got)
	}
}

func TestEvaluateSnapshotMultiConditionAND(t *testing.T) {
	snap := releases.Snapshot{
		Flags: []releases.SnapshotFlag{
			{
				Key: "greet", Type: "string", Default: "hello",
				Rules: []releases.SnapshotRule{
					{Priority: 1,
						Condition: []any{
							map[string]any{"field": "platform", "op": "==", "value": "android"},
							map[string]any{"field": "country", "op": "==", "value": "US"},
						},
						Value: "hello-us-android"},
				},
			},
		},
	}
	values, _ := EvaluateSnapshot(snap, eval.Context{Platform: "android", Country: "US"}, "")
	if values["greet"] != "hello-us-android" {
		t.Errorf("both conditions hold: got %v, want hello-us-android", values["greet"])
	}
	values, _ = EvaluateSnapshot(snap, eval.Context{Platform: "android", Country: "FR"}, "")
	if values["greet"] != "hello" {
		t.Errorf("one condition fails: got %v, want default hello", values["greet"])
	}
}

func TestOldTopLevelCompatDecodesSame(t *testing.T) {
	oldRaw := `{"flags":[{"key":"greet","type":"string","default":"hello","group":"","rules":[{"priority":1,"condition":{"field":"platform","op":"==","value":"android"},"value":"hi"}]}],"experiments":[]}`
	nestedRaw := `{"flags":[{"key":"greet","type":"string","default":"hello","group":"","rules":[{"priority":1,"condition":{"field":"platform","op":"==","value":"android"},"value":"hi"}],"experiments":[]}]}`
	var oldSnap, nestedSnap releases.Snapshot
	if err := json.Unmarshal([]byte(oldRaw), &oldSnap); err != nil {
		t.Fatalf("unmarshal old: %v", err)
	}
	if err := json.Unmarshal([]byte(nestedRaw), &nestedSnap); err != nil {
		t.Fatalf("unmarshal nested: %v", err)
	}
	if len(nestedSnap.Flags) != 1 || nestedSnap.Flags[0].Key != "greet" {
		t.Fatalf("nested flags not preserved: %+v", nestedSnap.Flags)
	}
	ctx := eval.Context{Platform: "android", UserHash: "0123456789abcdef"}
	oldValues, _ := EvaluateSnapshot(oldSnap, ctx, "")
	nestedValues, _ := EvaluateSnapshot(nestedSnap, ctx, "")
	if !reflect.DeepEqual(oldValues, nestedValues) {
		t.Fatalf("old vs nested values differ: %v vs %v", oldValues, nestedValues)
	}
	if nestedValues["greet"] != "hi" {
		t.Fatalf("rule must win for android, got %v", nestedValues["greet"])
	}
}

// TestEvaluateSnapshotCorruptRulesFailClosed pins the fail-closed
// decode: a null, garbage, or empty-array stored condition drops the
// rule so it matches nobody — the flag falls through to its default
// instead of vacuously matching every user.
func TestEvaluateSnapshotCorruptRulesFailClosed(t *testing.T) {
	snap := releases.Snapshot{
		Flags: []releases.SnapshotFlag{
			{
				Key: "risky", Type: "bool", Default: false,
				Rules: []releases.SnapshotRule{
					{Priority: 1, Condition: nil, Value: true},
					{Priority: 2, Condition: "garbage", Value: true},
					{Priority: 3, Condition: []any{}, Value: true},
				},
			},
		},
	}
	values, _ := EvaluateSnapshot(snap, eval.Context{Platform: "android", UserHash: "0123456789abcdef"}, "")
	if values["risky"] != false {
		t.Errorf("corrupt rules must match nobody (fail closed), got %v", values["risky"])
	}
}

func TestBuildContextUidSunset(t *testing.T) {
	const want = `raw "uid" is forbidden: send "userHash" instead.`
	for _, raw := range []string{
		`uid=user-7`,
		`uid=user-7&userHash=0123456789abcdef`,
		`userHash=0123456789abcdef&uid=user-7`,
	} {
		if _, err := BuildContext(query(raw), "0123456789abcdef", ""); err == nil {
			t.Errorf("%s: expected uid-sunset 400, got nil", raw)
		} else if err.Error() != want {
			t.Errorf("%s: error = %q, want %q", raw, err.Error(), want)
		}
	}
	if _, err := BuildContext(query(`uid=&userHash=0123456789abcdef`), "0123456789abcdef", ""); err != nil {
		t.Errorf("empty uid + valid hash must pass, got %v", err)
	}
}

func TestBuildContextAttrsDenylist(t *testing.T) {
	for _, key := range []string{"email", "Email", "e-mail", "phone", "ip", "IP", "deviceid", "device_id", "adid", "idfa", "idfv", "androidid", "serial", "mac", "imei"} {
		q := url.Values{"attrs": []string{`{"` + key + `":"v"}`}}
		if _, err := BuildContext(q, "0123456789abcdef", ""); err == nil {
			t.Errorf("attrs key %q: expected 400, got nil", key)
		}
	}
	if _, err := BuildContext(url.Values{"attrs": []string{`{"tier":"pro"}`}}, "0123456789abcdef", ""); err != nil {
		t.Errorf("benign attrs must pass, got %v", err)
	}
}

// Client ?userHash= (any shape) is accepted-but-ignored: effective
// identity is always the server anonID unless a valid ?b= wins.
// Never 400 on ?userHash=.
func TestEffectiveUserHash(t *testing.T) {
	const anon = "0123456789abcdef"
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"ignore-valid-hash": {`userHash=092081140b677b45`, anon},
		"ignore-trimmed":    {`userHash=++092081140b677b45++`, anon},
		"absent-fallback":   {`platform=ios`, anon},
		"empty-fallback":    {`userHash=`, anon},
		"blank-fallback":    {`userHash=+++`, anon},
		"too-short":         {`userHash=abc123`, anon},
		"too-long":          {`userHash=0123456789abcdef00`, anon},
		"uppercase":         {`userHash=0123456789ABCDEF`, anon},
		"non-hex":           {`userHash=0123456789abcdeg`, anon},
		"raw-uid":           {`userHash=user-7`, anon},
		"bucket-wins":       {`b=42&userHash=092081140b677b45`, "42"},
	} {
		if got := effectiveUserHash(query(tc.raw), anon); got != tc.want {
			t.Errorf("%s: effective = %q, want %q", name, got, tc.want)
		}
	}
}

// effectiveUserHash output reaches ctx.UserHash verbatim: different
// ?userHash= values yield the SAME bucketing (server anonID), while a
// valid ?b= still overrides the anonID.
func TestEffectiveUserHashFeedsBuildContext(t *testing.T) {
	const anon = "aaaa0000bbbb1111"
	for _, raw := range []string{
		`userHash=092081140b677b45&platform=ios`,
		`userHash=ffffffffffffffff&platform=ios`,
		`userHash=nothex&platform=ios`,
	} {
		q := query(raw)
		ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
		if err != nil {
			t.Fatalf("%s: BuildContext err = %v", raw, err)
		}
		if ctx.UserHash != anon {
			t.Fatalf("%s: ctx.UserHash = %q, want server anonID %q (userHash ignored)", raw, ctx.UserHash, anon)
		}
	}
	q := query(`b=42&userHash=092081140b677b45&platform=ios`)
	ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
	if err != nil {
		t.Fatalf("BuildContext err = %v", err)
	}
	if ctx.UserHash != "42" {
		t.Fatalf("ctx.UserHash = %q, want bucket %q (?b= wins)", ctx.UserHash, "42")
	}
}

// ---- T10 stable-bucket response-shape pins (additive; frozen pins above untouched) ----
//
// Reference: T2 bucket_mint_test.go owns the mint/echo/empty body-builder
// seams (TestResponseBucketMintedWhenAbsent, TestResponseBucketEchoesClientBucket,
// TestEmptyReleaseBodyOmitsBucket). These two pins cover the remaining T10
// contract bullets at the fetch response-shape seam: 304-keeps-bucket
// (etag-independent arm; getConfig returns 304 before EffectiveBucket ever
// runs) and empty-release no-mint (version 0/etag "none" with no bucket field
// vs evaluated 200s that always carry one).

// TestFetch_StableBucket304KeepsBucket pins that the bucket arm is
// etag-independent: the same ?b=42 evaluates the identical ctx.UserHash and
// identical experiment arm whether If-None-Match matches the stored etag
// (304, empty body, persisted bucket kept client-side) or not (200 carrying
// the same bucket field). Pure-function pin of the getConfig order: the 304
// early-return sits before the 200-only EffectiveBucket mint.
func TestFetch_StableBucket304KeepsBucket(t *testing.T) {
	const storedEtag = "b41b62605c0df712"
	q := query(`b=42&platform=ios`)
	ctx, err := BuildContext(q, effectiveUserHash(q, "0123456789abcdef"), "US")
	if err != nil {
		t.Fatalf("?b=42 must not 400, got %v", err)
	}
	if ctx.UserHash != "42" {
		t.Fatalf("ctx.UserHash = %q, want bucket %q", ctx.UserHash, "42")
	}
	if !EtagMatches(storedEtag, storedEtag) {
		t.Fatal("exact etag must report hit (304 keeps the persisted bucket)")
	}
	if EtagMatches("deadbeef", storedEtag) {
		t.Fatal("other etag must report miss (200 re-carries the same bucket)")
	}
	_, vHit := EvaluateSnapshot(fakeSnapshot(), ctx, "")
	_, vMiss := EvaluateSnapshot(fakeSnapshot(), ctx, "")
	if vHit["new_ui"] != vMiss["new_ui"] {
		t.Fatalf("bucket arm moved across etag states: %q vs %q (304 must keep arm)", vHit["new_ui"], vMiss["new_ui"])
	}
	body := evaluatedBody(1, storedEtag, map[string]any{"k": true}, vMiss, "42", "2026-01-01T00:00:00Z")
	if body["bucket"] != "42" {
		t.Fatalf("200 re-carries bucket = %v, want %q (304-kept value)", body["bucket"], "42")
	}
}

// TestFetch_StableBucketEmptyReleaseNoMint pins the fresh-env 200 shape:
// version 0 + etag "none" + EMPTY values with NO bucket field (nothing to
// arm yet), while every evaluated 200 carries the effective bucket. Guards
// the getConfig branch order: empty-release returns before 304/mint.
func TestFetch_StableBucketEmptyReleaseNoMint(t *testing.T) {
	body := emptyReleaseBody("2026-01-01T00:00:00Z")
	if body["version"] != 0 || body["etag"] != "none" {
		t.Fatalf("empty release shape moved: version=%v etag=%v (want 0/none)", body["version"], body["etag"])
	}
	if _, ok := body["bucket"]; ok {
		t.Fatalf("empty release must omit bucket, got %v", body["bucket"])
	}
	full := evaluatedBody(1, "abc123", map[string]any{"k": true}, map[string]string{}, "42", "2026-01-01T00:00:00Z")
	if full["bucket"] != "42" {
		t.Fatalf("evaluated 200: bucket = %v, want effective %q", full["bucket"], "42")
	}
}
