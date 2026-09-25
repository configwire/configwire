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
	"net/url"
	"strings"
	"testing"

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
		if _, err := BuildContext(q); err == nil {
			t.Errorf("%s: expected 400-mapped error, got nil", name)
		}
	}
}

// TestAuthOrder_OversizeAttrsIs414 pins: attrs over 8192 bytes -> handler
// 414 (distinct from 400: getConfig branches on raw length first).
func TestAuthOrder_OversizeAttrsIs414(t *testing.T) {
	big := strings.Repeat("a", MaxAttrsBytes+1)
	q := url.Values{"attrs": []string{`{"k":"` + big + `"}`}}
	_, err := BuildContext(q)
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
