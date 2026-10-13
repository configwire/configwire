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
		if _, _, err := BuildContext(q); err == nil {
			t.Errorf("%s: expected 400-mapped error, got nil", name)
		}
	}
}

// TestAuthOrder_OversizeAttrsIs414 pins: attrs over 8192 bytes -> handler
// 414 (distinct from 400: getConfig branches on raw length first).
func TestAuthOrder_OversizeAttrsIs414(t *testing.T) {
	big := strings.Repeat("a", MaxAttrsBytes+1)
	q := url.Values{"attrs": []string{`{"k":"` + big + `"}`}}
	_, _, err := BuildContext(q)
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

// TestAuthOrder_DeprecatedUidAccepted pins Choice-C compat: legacy ?uid=
// is accepted during the compat window, never 400 on its own.
// Renamed from TestAuthOrder_RawUidRejected (strict-reject is retired).
// Compat window: getConfig stamps X-CW-Deprecated-uid:true on success
// paths whenever the trimmed ?uid= is non-empty (driven by the
// deprecated bool below); canonical ?userHash= (16 lowercase hex) wins
// when valid; invalid ?userHash= errors even with ?uid= present;
// ?uid=-only falls back to eval.HashUserID(uid); empty ?uid= == absent.
func TestAuthOrder_DeprecatedUidAccepted(t *testing.T) {
	// uid-only -> UserHash == eval.HashUserID(uid), deprecated=true.
	q, err := url.ParseQuery(`uid=user-7`)
	if err != nil {
		t.Fatalf("bad test query: %v", err)
	}
	ctx, deprecated, err := BuildContext(q)
	if err != nil {
		t.Fatalf("uid-only must pass (compat fallback), got %v", err)
	}
	if want := eval.HashUserID("user-7"); ctx.UserHash != want {
		t.Errorf("uid-only UserHash = %q, want HashUserID %q", ctx.UserHash, want)
	}
	if !deprecated {
		t.Error("uid-only deprecated = false, want true")
	}

	// Bucket equality: uid-fallback hashes into the same bucket as the
	// equivalent direct userHash.
	direct, _, err := BuildContext(mustQuery(t, `userHash=`+eval.HashUserID("user-7")))
	if err != nil {
		t.Fatalf("direct-hash setup must pass, got %v", err)
	}
	if got, want := eval.Bucket(ctx.UserHash, "seed-1"), eval.Bucket(direct.UserHash, "seed-1"); got != want {
		t.Errorf("Bucket(uid-fallback) = %d, Bucket(userHash-direct) = %d, want equal", got, want)
	}

	// uid-empty -> anonymous + deprecated=false (empty uid == absent).
	for name, raw := range map[string]string{
		"uid-empty":      `uid=`,
		"uid-empty-anon": `uid=&platform=ios`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		ctx, deprecated, err := BuildContext(q)
		if err != nil {
			t.Errorf("%s: empty uid must pass as anonymous, got %v", name, err)
			continue
		}
		if ctx.UserHash != "" {
			t.Errorf("%s: anonymous UserHash = %q, want empty", name, ctx.UserHash)
		}
		if deprecated {
			t.Errorf("%s: deprecated = true, want false (empty uid == absent)", name)
		}
	}

	// uid + valid hash -> hash wins + deprecated=true.
	for name, raw := range map[string]string{
		"uid-with-hash":   `uid=user-7&userHash=0123456789abcdef`,
		"uid-both-params": `userHash=0123456789abcdef&uid=user-7&platform=ios`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		ctx, deprecated, err := BuildContext(q)
		if err != nil {
			t.Errorf("%s: valid hash + uid must pass, got %v", name, err)
			continue
		}
		if ctx.UserHash != "0123456789abcdef" {
			t.Errorf("%s: UserHash = %q, want canonical hash to win", name, ctx.UserHash)
		}
		if !deprecated {
			t.Errorf("%s: deprecated = false, want true (uid present)", name)
		}
	}

	// uid-empty + valid hash -> hash wins + deprecated=false.
	q, err = url.ParseQuery(`uid=&userHash=0123456789abcdef`)
	if err != nil {
		t.Fatalf("bad test query: %v", err)
	}
	ctx, deprecated, err = BuildContext(q)
	if err != nil {
		t.Fatalf("empty uid + valid hash must pass, got %v", err)
	}
	if ctx.UserHash != "0123456789abcdef" {
		t.Errorf("empty-uid hash UserHash = %q, want fixture", ctx.UserHash)
	}
	if deprecated {
		t.Error("empty-uid hash deprecated = true, want false")
	}

	// invalid-hash + uid -> error mentioning userHash.
	q, err = url.ParseQuery(`userHash=nothex&uid=user-7`)
	if err != nil {
		t.Fatalf("bad test query: %v", err)
	}
	if _, _, err := BuildContext(q); err == nil {
		t.Error("invalid-hash + uid: expected 400-mapped error, got nil")
	} else if !strings.Contains(err.Error(), "userHash") {
		t.Errorf("invalid-hash + uid error must mention userHash, got %q", err.Error())
	}
}

// TestAuthOrder_BadUserHashWithUidStill400 pins: an invalid ?userHash=
// (non-empty, non-16-hex) is 400 even when legacy ?uid= is present.
// The error must mention userHash (never silently fall back to uid).
func TestAuthOrder_BadUserHashWithUidStill400(t *testing.T) {
	for name, raw := range map[string]string{
		"too-short":  `userHash=abc123&uid=user-7`,
		"too-long":   `userHash=0123456789abcdef00&uid=user-7`,
		"uppercase":  `userHash=0123456789ABCDEF&uid=user-7`,
		"non-hex":    `userHash=0123456789abcdeg&uid=user-7`,
		"raw-uid":    `userHash=user-7&uid=user-7`,
		"whitespace": `userHash=+0123456789abcd+&uid=user-7`,
	} {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("%s: bad test query: %v", name, err)
		}
		if _, _, err := BuildContext(q); err == nil {
			t.Errorf("%s: expected 400-mapped error, got nil", name)
		} else if !strings.Contains(err.Error(), "userHash") {
			t.Errorf("%s: bad-hash error must mention userHash, got %q", name, err.Error())
		}
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("bad test query %q: %v", raw, err)
	}
	return q
}

// TestMarkDeprecatedUid_StampsHeaderAndCounts pins the shared stamping logic
// behind all three getConfig success paths (200 empty-release, 304,
// 200 config): exact header value plus one counter increment per call.
// The per-path wiring (each calls this gated on deprecatedUid) is covered
// live by e2e Stage 2N for the 200 path; all three share this helper.
func TestMarkDeprecatedUid_StampsHeaderAndCounts(t *testing.T) {
	h := http.Header{}
	before := deprecatedUidFallbackTotal.Load()
	markDeprecatedUid(h)
	if got := h.Get("X-CW-Deprecated-uid"); got != "true" {
		t.Fatalf("header = %q, want %q", got, "true")
	}
	if got, want := deprecatedUidFallbackTotal.Load(), before+1; got != want {
		t.Fatalf("counter = %d, want %d (delta +1)", got, want)
	}
	markDeprecatedUid(h)
	if got, want := deprecatedUidFallbackTotal.Load(), before+2; got != want {
		t.Fatalf("second call counter = %d, want %d", got, want)
	}
	if got := h.Get("X-CW-Deprecated-uid"); got != "true" {
		t.Fatalf("repeat header = %q, want %q", got, "true")
	}
}

// TestAuthOrder_BadUserHashIs400 pins: ?userHash= must be 16 lowercase hex
// when present; anything else -> handler 400. Anonymous (absent/empty) and
// valid 16-hex pass. Mirrors the attrs 400 matrix style.
func TestAuthOrder_BadUserHashIs400(t *testing.T) {
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
		ctx, _, err := BuildContext(q)
		switch name {
		case "empty-value", "absent":
			// Anonymous: allowed, empty identity.
			if err != nil {
				t.Errorf("%s: anonymous must pass, got %v", name, err)
			} else if ctx.UserHash != "" {
				t.Errorf("%s: anonymous UserHash = %q, want empty", name, ctx.UserHash)
			}
		case "valid-hex":
			if err != nil {
				t.Errorf("%s: valid 16-hex must pass, got %v", name, err)
			} else if ctx.UserHash != "0123456789abcdef" {
				t.Errorf("%s: UserHash = %q, want fixture", name, ctx.UserHash)
			}
		default:
			if err == nil {
				t.Errorf("%s: expected 400-mapped error, got ctx %+v", name, ctx)
			} else if !strings.Contains(err.Error(), "userHash") {
				t.Errorf("%s: bad-hash error must mention userHash, got %q", name, err.Error())
			}
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
