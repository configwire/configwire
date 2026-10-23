// Baseline characterization for todo 11 (fetch-lookups hardening).
//
// These tests pin the fetch auth-order matrix through its pure seams on
// UNCHANGED code. They must pass both before and after the indexed-lookup
// fix: any failure means the wire contract moved.
//
// Matrix (fetch.go getConfig order + CONTRACT.md section 1):
//  1. 401 (key) via ingest.RequireSDKKey — header + prefix/hash seams below.
//  2. 404 unknown slug / 400 ambiguous via envresolve (see envresolve pkg).
//  3. 401 scope mismatch via envresolve.ErrScopeMismatch (see envresolve pkg).
//  4. 400 malformed attrs / 414 oversize attrs via BuildContext.
//  5. 200 empty release (version 0, etag "none") when no release row.
//  6. 304 iff If-None-Match EXACTLY equals the stored etag.
package fetch

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/ingest"
	"github.com/configwire/configwire/releases"
)

// TestAuthOrder_KeyHeaderContract pins the SDK-key header name. A rename
// here silently turns every authed fetch into a 401.
func TestAuthOrder_KeyHeaderContract(t *testing.T) {
	if ingest.HeaderKey != "X-ConfigWire-Key" {
		t.Fatalf("SDK key header = %q, want X-ConfigWire-Key", ingest.HeaderKey)
	}
}

// TestAuthOrder_MalformedAttrsIs400 pins: malformed attrs (bad JSON, or
// valid JSON that is not an object) -> handler 400. BuildContext reports
// the error; getConfig maps it via re.BadRequestError.
func TestAuthOrder_MalformedAttrsIs400(t *testing.T) {
	for name, raw := range map[string]string{
		"not-json":  `attrs=notjson`,
		"array":     `attrs=%5B1%2C2%5D`,
		"string":    `attrs=%22hi%22`,
		"number":    `attrs=42`,
		"null":      `attrs=null`,
		"truncated": `attrs=%7B%22a%22%3A`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		if _, err := BuildContext(q, "0123456789abcdef", ""); err == nil {
			t.Errorf("%s: expected 400-mapped error, got nil", name)
		}
	}
}

// TestAuthOrder_ForbiddenAttrsIs400 pins the PII denylist: top-level attrs
// keys matching the denylist (case-insensitive) are 400 with a PII-free
// error (key named, value never logged); benign keys pass.
func TestAuthOrder_ForbiddenAttrsIs400(t *testing.T) {
	denied := []string{
		"email", "Email", "EMAIL", "e-mail", "E-Mail", "phone", "PHONE",
		"ip", "IP", "deviceid", "DeviceId", "device_id", "DEVICE_ID",
		"advertisingid", "adid", "ADID", "idfa", "IDFA", "idfv", "IDFV",
		"androidid", "serial", "mac", "MAC", "imei", "IMEI",
	}
	for _, key := range denied {
		attrs := `{"` + key + `":"x"}`
		q := url.Values{"attrs": []string{attrs}}
		if _, err := BuildContext(q, "0123456789abcdef", ""); err == nil {
			t.Errorf("attrs key %q: expected 400-mapped error, got nil", key)
		} else if strings.Contains(err.Error(), "x") && len(key) == 1 {
			t.Errorf("attrs key %q: error must not echo the value, got %q", key, err.Error())
		}
	}
	// Benign keys still pass.
	q := url.Values{"attrs": []string{`{"tier":"pro","n":7}`}}
	if _, err := BuildContext(q, "0123456789abcdef", ""); err != nil {
		t.Errorf("benign attrs must pass, got %v", err)
	}
}

// TestAuthOrder_OversizeAttrsIs414 pins: attrs over 8192 bytes -> handler
// 414 (distinct from 400: getConfig branches on raw length first).
func TestAuthOrder_OversizeAttrsIs414(t *testing.T) {
	big := strings.Repeat("a", MaxAttrsBytes+1)
	q := url.Values{"attrs": []string{`{"k":"` + big + `"}`}}
	_, err := BuildContext(q, "0123456789abcdef", "")
	if err == nil {
		t.Fatal("expected oversize error, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("414-mapped error must mention size, got %q", err.Error())
	}
	if len(q.Get("attrs")) <= MaxAttrsBytes {
		t.Fatal("test setup wrong: raw attrs must exceed the bound (handler checks raw length)")
	}
}

// TestAuthOrder_RawUidSunsetIs400 pins the ?uid= sunset: any trimmed
// non-empty ?uid= is 400 with the sunset message, even alongside a valid
// userHash; empty ?uid= == absent (anonymous passes).
func TestAuthOrder_RawUidSunsetIs400(t *testing.T) {
	const want = `raw "uid" is forbidden: send "userHash" instead.`
	for name, raw := range map[string]string{
		"uid-only":       `uid=user-7`,
		"uid-with-hash":  `uid=user-7&userHash=0123456789abcdef`,
		"hash-with-uid":  `userHash=0123456789abcdef&uid=user-7&platform=ios`,
		"uid-spaces":     `uid=++user-7++`,
		"uid-invalid-h":  `userHash=nothex&uid=user-7`,
		"uid-bad-hash":   `userHash=abc123&uid=user-7`,
		"uid-upper-hash": `userHash=0123456789ABCDEF&uid=user-7`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		if _, err := BuildContext(q, "0123456789abcdef", ""); err == nil {
			t.Errorf("%s: expected 400-mapped uid-sunset error, got nil", name)
		} else if err.Error() != want {
			t.Errorf("%s: error = %q, want %q", name, err.Error(), want)
		}
	}
	// Empty uid == absent: server anonID still stamps (bucketing is
	// server-derived, never anonymous-empty on the success path).
	for name, raw := range map[string]string{
		"uid-empty":      `uid=`,
		"uid-empty-anon": `uid=&platform=ios`,
		"uid-blank":      `uid=+++`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		ctx, err := BuildContext(q, "0123456789abcdef", "")
		if err != nil {
			t.Errorf("%s: empty uid must pass, got %v", name, err)
			continue
		}
		if ctx.UserHash != "0123456789abcdef" {
			t.Errorf("%s: UserHash = %q, want server anonID", name, ctx.UserHash)
		}
	}
	q, err := url.ParseQuery(`uid=&userHash=0123456789abcdef`)
	if err != nil {
		t.Fatalf("bad test query: %v", err)
	}
	ctx, err := BuildContext(q, "aaaa0000bbbb1111", "")
	if err != nil {
		t.Fatalf("empty uid + client hash must pass, got %v", err)
	}
	if ctx.UserHash != "aaaa0000bbbb1111" {
		t.Errorf("empty-uid UserHash = %q, want server anonID", ctx.UserHash)
	}
}

// TestAuthOrder_BadUserHashIgnored pins the server-wins cutover: a
// malformed ?userHash= alone is accepted-but-ignored (server anonID
// wins, never 400); with ?uid= present the uid-sunset error wins (uid
// is rejected first, never silently hashed).
func TestAuthOrder_BadUserHashWithUidStill400(t *testing.T) {
	const anon = "0123456789abcdef"
	for name, raw := range map[string]string{
		"too-short":  `userHash=abc123`,
		"too-long":   `userHash=0123456789abcdef00`,
		"uppercase":  `userHash=0123456789ABCDEF`,
		"non-hex":    `userHash=0123456789abcdeg`,
		"raw-uid":    `userHash=user-7`,
		"whitespace": `userHash=+0123456789abcd+`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		ctx, err := BuildContext(q, anon, "")
		if err != nil {
			t.Errorf("%s: client userHash must be accepted-but-ignored, got %v", name, err)
		} else if ctx.UserHash != anon {
			t.Errorf("%s: UserHash = %q, want server anonID %q", name, ctx.UserHash, anon)
		}
	}
	// uid present alongside a bad hash: uid-sunset error (rejected first).
	q, err := url.ParseQuery(`userHash=abc123&uid=user-7`)
	if err != nil {
		t.Fatalf("bad test query: %v", err)
	}
	if _, err := BuildContext(q, anon, ""); err == nil {
		t.Error("bad-hash + uid: expected 400-mapped error, got nil")
	} else if err.Error() != `raw "uid" is forbidden: send "userHash" instead.` {
		t.Errorf("bad-hash + uid error = %q, want uid-sunset message", err.Error())
	}
}

// TestMarkDeprecatedSnapshot_StampsHeaderAndCounts mirrors the uid stamping
// test for legacy-shape snapshots: exact header value plus one counter
// increment per call; nested-only payloads never stamp (detector false).
func TestMarkDeprecatedSnapshot_StampsHeaderAndCounts(t *testing.T) {
	h := http.Header{}
	before := deprecatedSnapshotTotal.Load()
	markDeprecatedSnapshot(h)
	if got := h.Get("X-CW-Deprecated-snapshot"); got != "true" {
		t.Fatalf("header = %q, want %q", got, "true")
	}
	if got, want := deprecatedSnapshotTotal.Load(), before+1; got != want {
		t.Fatalf("counter = %d, want %d (delta +1)", got, want)
	}
	if releases.IsLegacyTopLevel([]byte(`{"flags":[]}`)) {
		t.Fatal("nested-only payload must not detect as legacy (no header)")
	}
	if !releases.IsLegacyTopLevel([]byte(`{"flags":[],"experiments":[{"id":"e1","flag":"f","seed":"s","variants":[],"status":"running"}]}`)) {
		t.Fatal("non-empty top-level experiments payload must detect as legacy")
	}
	if releases.IsLegacyTopLevel([]byte(`{"flags":[],"experiments":[]}`)) {
		t.Fatal("empty top-level experiments array must not detect as legacy")
	}
	if releases.IsLegacyTopLevel([]byte(`not json`)) {
		t.Fatal("malformed payload must not detect as legacy")
	}
	if got := h.Get("X-CW-Deprecated-snapshot"); got != "true" {
		t.Fatalf("repeat header = %q, want %q", got, "true")
	}
}

// TestAuthOrder_BuildContextStoresVerbatim pins that BuildContext stores
// the caller-resolved value verbatim: it never reads ?userHash= itself,
// so every client ?userHash= shape (valid, malformed, empty, absent)
// yields ctx.UserHash == anon when anon is passed through. Honoring a
// valid hash is the caller's job — getConfig passes
// effectiveUserHash(q, anonID) (identity.ResolveHonoredHash) as anonID,
// mirrored in ingest by deriveIdentity — so bucketing honors a valid
// ?userHash= at the system level; this seam only stores what it is given.
func TestAuthOrder_BuildContextStoresVerbatim(t *testing.T) {
	const anon = "0123456789abcdef"
	for name, raw := range map[string]string{
		"too-short":   `userHash=abc123`,
		"too-long":    `userHash=0123456789abcdef00`,
		"uppercase":   `userHash=0123456789ABCDEF`,
		"non-hex":     `userHash=0123456789abcdeg`,
		"raw-uid":     `userHash=user-7`,
		"spaces":      `userHash=+0123456789abcd+`,
		"empty-value": `userHash=`,
		"absent":      `platform=ios`,
		"valid-hex":   `userHash=0123456789abcdef`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		ctx, err := BuildContext(q, anon, "")
		if err != nil {
			t.Errorf("%s: BuildContext must store caller-resolved value verbatim (never 400 on userHash), got %v", name, err)
		} else if ctx.UserHash != anon {
			t.Errorf("%s: UserHash = %q, want caller-resolved value %q", name, ctx.UserHash, anon)
		}
	}
	// Verbatim, not ignoring: whatever the caller resolved (e.g. a valid
	// hash honored via effectiveUserHash/ResolveHonoredHash) is stored as-is.
	q, err := url.ParseQuery(`userHash=092081140b677b45`)
	if err != nil {
		t.Fatalf("bad test query: %v", err)
	}
	resolved := effectiveUserHash(q, anon)
	if resolved == anon {
		t.Fatalf("test setup wrong: valid hash must resolve to itself, got fallback %q", resolved)
	}
	ctx, err := BuildContext(q, resolved, "")
	if err != nil {
		t.Fatalf("honored hash must not error, got %v", err)
	}
	if ctx.UserHash != resolved {
		t.Errorf("UserHash = %q, want caller-resolved honored hash %q", ctx.UserHash, resolved)
	}
}

// Honored valid ?userHash= resolves to itself; absent/malformed falls
// back, never 400.
func TestAuthOrder_HonoredUserHash(t *testing.T) {
	const anon = "0123456789abcdef"
	honored := map[string]string{
		"valid-hex":      `userHash=092081140b677b45`,
		"all-f":          `userHash=ffffffffffffffff`,
		"all-digits":     `userHash=0000000000000000`,
		"trimmed":        `userHash=++092081140b677b45++`,
		"with-attrs":     `userHash=092081140b677b45&platform=ios`,
		"with-uid-empty": `userHash=092081140b677b45&uid=`,
	}
	for name, raw := range honored {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		if got := effectiveUserHash(q, anon); got != strings.TrimSpace(q.Get("userHash")) {
			t.Errorf("%s: effective = %q, want honored hash %q", name, got, strings.TrimSpace(q.Get("userHash")))
		}
		if _, err := BuildContext(q, effectiveUserHash(q, anon), ""); err != nil {
			t.Errorf("%s: honored hash must not error, got %v", name, err)
		}
	}
	fallback := map[string]string{
		"absent":      `platform=ios`,
		"empty-value": `userHash=`,
		"blank":       `userHash=+++`,
		"too-short":   `userHash=abc123`,
		"too-long":    `userHash=0123456789abcdef00`,
		"uppercase":   `userHash=0123456789ABCDEF`,
		"non-hex":     `userHash=0123456789abcdeg`,
		"raw-uid":     `userHash=user-7`,
	}
	for name, raw := range fallback {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		if got := effectiveUserHash(q, anon); got != anon {
			t.Errorf("%s: effective = %q, want fallback anonID %q", name, got, anon)
		}
		if _, err := BuildContext(q, effectiveUserHash(q, anon), ""); err != nil {
			t.Errorf("%s: malformed userHash must be 200-eligible (no 400), got %v", name, err)
		}
	}
}

// TestAuthOrder_EtagExactMatchIs304 pins the 304 rule: exact string match
// only. Weak (W/-prefixed), quoted, case- or whitespace-shifted values
// must NOT match (stored etags are bare hex, never quoted).
func TestAuthOrder_EtagExactMatchIs304(t *testing.T) {
	if !EtagMatches("b41b62605c0df712", "b41b62605c0df712") {
		t.Fatal("exact etag match must report hit (304)")
	}
	for _, h := range []string{"", "W/\"b41b62605c0df712\"", `"b41b62605c0df712"`, "B41B62605C0DF712", "b41b62605c0df712 "} {
		if EtagMatches(h, "b41b62605c0df712") {
			t.Errorf("header %q must NOT match (exact only, else 200)", h)
		}
	}
}

// TestAuthOrder_EmptyReleaseShape pins the fresh-env contract: an env with
// no release row answers 200 with version 0 and etag "none" — never 404 —
// with EMPTY values (SDKs fall back to in-app defaults).
func TestAuthOrder_EmptyReleaseShape(t *testing.T) {
	if emptyReleaseEtag != "none" {
		t.Fatalf("empty-release etag = %q, want %q", emptyReleaseEtag, "none")
	}
}

// TestAuthOrder_CORSDefault pins the permissive dev default; production
// restricts via CONFIGWIRE_CORS_ORIGIN. Every fetch response (incl. 304)
// carries this origin.
func TestAuthOrder_CORSDefault(t *testing.T) {
	t.Setenv("CONFIGWIRE_CORS_ORIGIN", "")
	if got := corsOrigin(); got != "*" {
		t.Fatalf("unset: corsOrigin = %q, want dev default %q", got, "*")
	}
}

// ---- T10 stable-bucket contract pins (additive; frozen pins above untouched) ----
//
// Reference (matrices owned elsewhere, cited not duplicated): T1 bucket_test.go
// owns the exhaustive ?b= matrices (TestParseBucketCanonicalHonored,
// TestParseBucketMalformedIs400 15-value, TestBucketPrecedenceWinsOverUserHash,
// TestBucketMalformedIs400ViaBuildContext, TestBucketWithUidStill400UidWins,
// TestBucketNumberSeedEvaluation); T2 bucket_mint_test.go owns the mint/echo
// seams (TestResponseBucketMintedWhenAbsent, TestResponseBucketEchoesClientBucket);
// T8 compat_bucket_guard_test.go owns the old-client byte-identical arms. This
// block pins only the owner-confirmed contract surface: canonical honored, the
// 4-value malformed 400 matrix (abc/-1/10000/007), ?b= > ?userHash= arm,
// ?uid=-wins 400. 304-keeps-bucket and empty-release no-mint live in
// fetch_test.go (response-shape home) — see
// TestFetch_StableBucket304KeepsBucket and TestFetch_StableBucketEmptyReleaseNoMint.

// TestContract_StableBucketCanonicalHonored pins: canonical "0"/"42"/"9999"
// ?b= reaches ctx.UserHash verbatim (Bucket(number+seed) arm), even alongside
// a valid ?userHash= (precedence resolved by effectiveUserHash before
// BuildContext stores it).
func TestContract_StableBucketCanonicalHonored(t *testing.T) {
	const anon = "0123456789abcdef"
	for _, b := range []string{"0", "42", "9999"} {
		q := query(`b=` + b + `&userHash=092081140b677b45&platform=ios`)
		if got := effectiveUserHash(q, anon); got != b {
			t.Errorf("b=%s: effective = %q, want bucket %q (?b= wins)", b, got, b)
			continue
		}
		ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
		if err != nil {
			t.Errorf("b=%s: canonical bucket must not 400, got %v", b, err)
			continue
		}
		if ctx.UserHash != b {
			t.Errorf("b=%s: UserHash = %q, want canonical bucket %q", b, ctx.UserHash, b)
		}
	}
}

// TestContract_StableBucketMalformed400Matrix pins the owner-confirmed 400
// matrix: abc/-1/10000/007 -> BuildContext 400 with the single bucket message
// (mirrors the ?uid= sunset; no second validation rule anywhere).
func TestContract_StableBucketMalformed400Matrix(t *testing.T) {
	const anon = "0123456789abcdef"
	for _, b := range []string{"abc", "-1", "10000", "007"} {
		q := query(`b=` + b + `&platform=ios`)
		if _, err := BuildContext(q, effectiveUserHash(q, anon), "US"); err == nil {
			t.Errorf("b=%s: expected 400-mapped error, got nil", b)
		} else if err.Error() != wantBucketErr {
			t.Errorf("b=%s: error = %q, want %q", b, err.Error(), wantBucketErr)
		}
	}
}

// TestContract_StableBucketWinsOverUserHashArm pins the precedence flip at
// the arm level: ?b=42 + valid ?userHash= evaluates the Bucket("42"+seed)
// arm (eval.Assign on the canonical number), not the honored-hash arm.
func TestContract_StableBucketWinsOverUserHashArm(t *testing.T) {
	const anon = "0123456789abcdef"
	q := query(`b=42&userHash=092081140b677b45&platform=ios`)
	ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
	if err != nil {
		t.Fatalf("?b=+hash must not 400, got %v", err)
	}
	_, variants := EvaluateSnapshot(fakeSnapshot(), ctx, "")
	want := eval.Assign(eval.Experiment{
		Seed: "seed-1",
		Variants: []eval.Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}, "42")
	if variants["new_ui"] != want {
		t.Fatalf("?b= arm = %q, want Bucket(\"42\"+seed) arm %q (?b= wins)", variants["new_ui"], want)
	}
	if hon := eval.Assign(eval.Experiment{
		Seed: "seed-1",
		Variants: []eval.Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}, "092081140b677b45"); variants["new_ui"] == hon && hon != want {
		t.Fatalf("?b= arm collapsed onto honored-hash arm %q (precedence lost)", hon)
	}
}

// TestContract_StableBucketUidWins400 pins: ?b= (valid AND malformed) + any
// non-empty ?uid= -> 400 with the uid-sunset message (?uid= checked first,
// never the bucket rule).
func TestContract_StableBucketUidWins400(t *testing.T) {
	const anon = "0123456789abcdef"
	for _, raw := range []string{
		`b=42&uid=user-7`,
		`uid=user-7&b=42`,
		`b=abc&uid=user-7`,
	} {
		q := query(raw)
		if _, err := BuildContext(q, effectiveUserHash(q, anon), ""); err == nil {
			t.Errorf("%s: expected 400, got nil", raw)
		} else if err.Error() != wantUidErr {
			t.Errorf("%s: error = %q, want uid-wins %q", raw, err.Error(), wantUidErr)
		}
	}
}
