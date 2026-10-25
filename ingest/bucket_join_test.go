package ingest

// T7 events/ingest bucket join: deriveIdentity honors ?b= with the
// identical T1 validation/400 rule so fetch arms and exposure
// attribution resolve to the same identity string per install.
//
// Canonical check delegates to identity.ParseBucketParam via
// parseBucketCanonical (fetch imports ingest for RequireKeyEnv, so
// ingest cannot import fetch back — import cycle). Malformed ?b= ->
// 400; absent -> "" (unattributed, no anonID fallback); ?b= wins over
// ignored ?userHash=/?uid= (any shape accepted-but-ignored).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const wantBucketErr = `invalid "b": send "0"-"9999".`

// TestDeriveIdentityBucketHonored pins ?b= precedence: canonical
// ?b= wins verbatim, including over ignored ?userHash=/?uid=.
func TestDeriveIdentityBucketHonored(t *testing.T) {
	for name, tc := range map[string]struct {
		target string
		want   string
	}{
		"canonical-42":      {"/api/v1/env/dev/events?b=42", "42"},
		"canonical-0":       {"/api/v1/env/dev/events?b=0", "0"},
		"canonical-9999":    {"/api/v1/env/dev/events?b=9999", "9999"},
		"bucket-beats-hash": {"/api/v1/env/dev/events?b=42&userHash=092081140b677b45", "42"},
		"bucket-beats-uid":  {"/api/v1/env/dev/events?b=42&uid=user-7", "42"},
		"extra-keys":        {"/api/v1/env/dev/events?b=42&platform=ios", "42"},
	} {
		re := requestForIdentity(tc.target, nil)
		got, country, aerr := deriveIdentity(re)
		if aerr != nil {
			t.Errorf("%s: canonical ?b= must not error, got %v", name, aerr)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: identity = %q, want bucket %q", name, got, tc.want)
		}
		if country != "" {
			t.Errorf("%s: country = %q, want empty (no headers)", name, country)
		}
	}
}

// TestDeriveIdentityBucketMalformedIs400 pins the T1 parity rule:
// malformed ?b= maps to 400 with the canonical message, never a
// silent fallback.
func TestDeriveIdentityBucketMalformedIs400(t *testing.T) {
	for _, b := range []string{"abc", "-1", "10000", "007", "12.0", " 12 "} {
		re := requestForIdentity("/api/v1/env/dev/events?platform=ios", nil)
		q := re.Request.URL.Query()
		q.Set("b", b)
		re.Request.URL.RawQuery = q.Encode()
		_, _, aerr := deriveIdentity(re)
		if aerr == nil {
			t.Errorf("b=%q: expected 400-mapped error, got nil", b)
			continue
		}
		if aerr.Status != 400 {
			t.Errorf("b=%q: status = %d, want 400", b, aerr.Status)
		}
		if aerr.Msg != wantBucketErr {
			t.Errorf("b=%q: error = %q, want %q", b, aerr.Msg, wantBucketErr)
		}
	}
}

// TestDeriveIdentityBucketAbsentFallsBack pins the fallthrough:
// absent ?b= stores "" (unattributed); ?userHash=/?uid= of any shape
// are accepted-but-ignored and never become the bucket.
func TestDeriveIdentityBucketAbsentFallsBack(t *testing.T) {
	for name, target := range map[string]string{
		"absent-valid-hash":   "/api/v1/env/dev/events?userHash=092081140b677b45",
		"absent-malformed":    "/api/v1/env/dev/events?userHash=nothex",
		"absent-uid":          "/api/v1/env/dev/events?uid=user-7",
		"absent-blank-uid":    "/api/v1/env/dev/events?uid=+++",
		"absent-bare":         "/api/v1/env/dev/events?platform=ios",
		"absent-hash-and-uid": "/api/v1/env/dev/events?userHash=092081140b677b45&uid=user-7",
	} {
		re := requestForIdentity(target, nil)
		got, _, aerr := deriveIdentity(re)
		if aerr != nil {
			t.Fatalf("%s: absent ?b= must not error, got %v", name, aerr)
		}
		if got != "" {
			t.Fatalf("%s: identity = %q, want empty (no ?b=)", name, got)
		}
	}
}

// TestDeriveIdentityUidAcceptedButIgnored pins that ?uid= never becomes
// the bucket while the compat header still signals it was sent.
func TestDeriveIdentityUidAcceptedButIgnored(t *testing.T) {
	re := requestForIdentity("/api/v1/env/dev/events?uid=user-7", nil)
	got, _, aerr := deriveIdentity(re)
	if aerr != nil {
		t.Fatalf("uid path err = %v", aerr)
	}
	if got != "" {
		t.Fatalf("?uid= identity = %q, want empty (accepted-but-ignored)", got)
	}
	rec := httptest.NewRecorder()
	re.Response = rec
	stampUidCompat(re)
	if rec.Header().Get(uidCompatHeader) != "true" {
		t.Fatalf("?uid= must stamp %s=true, got %q", uidCompatHeader, rec.Header().Get(uidCompatHeader))
	}

	re = requestForIdentity("/api/v1/env/dev/events?b=42&uid=user-7", func(h http.Header) {
		h.Set("CF-IPCountry", "us")
	})
	got, country, aerr := deriveIdentity(re)
	if aerr != nil {
		t.Fatalf("bucket+uid err = %v", aerr)
	}
	if got != "42" || country != "US" {
		t.Fatalf("bucket+uid = (%q, %q), want (42, US)", got, country)
	}
	rec = httptest.NewRecorder()
	re.Response = rec
	stampUidCompat(re)
	if rec.Header().Get(uidCompatHeader) != "true" {
		t.Fatalf("bucket+uid must stamp %s=true, got %q", uidCompatHeader, rec.Header().Get(uidCompatHeader))
	}

	re = requestForIdentity("/api/v1/env/dev/events?platform=ios", nil)
	rec = httptest.NewRecorder()
	re.Response = rec
	stampUidCompat(re)
	if rec.Header().Get(uidCompatHeader) != "" {
		t.Fatalf("absent ?uid= must not stamp %s, got %q", uidCompatHeader, rec.Header().Get(uidCompatHeader))
	}
}

// TestFetchIngestBucketJoin is the paired join proof the package
// graph allows (fetch imports ingest, so this internal test cannot
// import fetch — cycle): the same ?b=42 canonical string fetch pins
// as ctx.UserHash (fetch.TestBucketNumberSeedEvaluation) must be
// the exact string deriveIdentity returns, so the fetch arm
// Bucket("42"+seed) and the exposure attribution join per install.
func TestFetchIngestBucketJoin(t *testing.T) {
	for _, b := range []string{"0", "42", "9999"} {
		re := requestForIdentity("/api/v1/env/dev/events?b="+b+"&userHash=092081140b677b45", nil)
		got, _, aerr := deriveIdentity(re)
		if aerr != nil {
			t.Fatalf("b=%s: err = %v", b, aerr)
		}
		if got != b {
			t.Fatalf("b=%s: deriveIdentity = %q, want canonical %q (fetch ctx.UserHash must match)", b, got, b)
		}
	}
}
