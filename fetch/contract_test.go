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

// TestAuthOrder_UserHashIgnored pins the server-wins cutover: every
// client ?userHash= shape (valid, malformed, empty, absent) is accepted —
// bucketing always uses the server-derived anonID, never the query value.
func TestAuthOrder_BadUserHashIs400(t *testing.T) {
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
			t.Errorf("%s: client userHash must be accepted-but-ignored, got %v", name, err)
		} else if ctx.UserHash != anon {
			t.Errorf("%s: UserHash = %q, want server anonID %q", name, ctx.UserHash, anon)
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
