package ingest

// T7 events/ingest bucket join: deriveIdentity honors ?b= with the
// identical T1 validation/400 rule so fetch arms and exposure
// attribution resolve to the same identity string per install.
//
// Canonical check is duplicated from fetch.ParseBucket (fetch imports
// ingest for RequireKeyEnv, so ingest cannot import fetch back —
// import cycle). Single rule: fetch.ParseBucket is canonical; the
// copy here (parseBucketCanonical) must stay byte-identical in
// behavior. Malformed ?b= -> 400; absent -> anonID (?userHash= of any
// shape is ignored); ?b= wins over an ignored ?userHash=.

import (
	"testing"
	"time"

	"github.com/configwire/configwire/identity"
	"github.com/configwire/configwire/limits"
)

const wantBucketErr = `invalid "b": send "0"-"9999".`

// TestDeriveIdentityBucketHonored pins ?b= precedence: canonical
// ?b= wins verbatim, including over an (ignored) ?userHash=.
func TestDeriveIdentityBucketHonored(t *testing.T) {
	SetIdentityManager(identity.NewManager(identity.NewMemoryStore()))
	now := time.Now().UTC()
	const envID = "env-bucket-join"

	for name, tc := range map[string]struct {
		target string
		want   string
	}{
		"canonical-42":      {"/api/v1/env/dev/events?b=42", "42"},
		"canonical-0":       {"/api/v1/env/dev/events?b=0", "0"},
		"canonical-9999":    {"/api/v1/env/dev/events?b=9999", "9999"},
		"bucket-beats-hash": {"/api/v1/env/dev/events?b=42&userHash=092081140b677b45", "42"},
		"extra-keys":        {"/api/v1/env/dev/events?b=42&platform=ios", "42"},
	} {
		re := requestForIdentity(tc.target, nil)
		got, country, aerr := deriveIdentity(re, envID, now)
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
	SetIdentityManager(identity.NewManager(identity.NewMemoryStore()))
	now := time.Now().UTC()
	const envID = "env-bucket-join"

	for _, b := range []string{"abc", "-1", "10000", "007", "12.0", " 12 "} {
		re := requestForIdentity("/api/v1/env/dev/events?platform=ios", nil)
		q := re.Request.URL.Query()
		q.Set("b", b)
		re.Request.URL.RawQuery = q.Encode()
		_, _, aerr := deriveIdentity(re, envID, now)
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
// absent ?b= keeps today's rule (ignored-?userHash= -> anonID).
func TestDeriveIdentityBucketAbsentFallsBack(t *testing.T) {
	SetIdentityManager(identity.NewManager(identity.NewMemoryStore()))
	now := time.Now().UTC()
	const envID = "env-bucket-join"

	// Absent ?b= + valid ?userHash= -> ignored, daily manager anonID wins.
	re := requestForIdentity("/api/v1/env/dev/events?userHash=092081140b677b45", nil)
	want, err := idManager.AnonID(envID, limits.ClientIP(re), re.Request.UserAgent(), now)
	if err != nil {
		t.Fatalf("manager anonID err = %v", err)
	}
	got, _, aerr := deriveIdentity(re, envID, now)
	if aerr != nil {
		t.Fatalf("ignored path err = %v", aerr)
	}
	if got != want || got == "" {
		t.Fatalf("absent ?b= + valid hash: identity = %q, want daily anonID %q (userHash ignored)", got, want)
	}

	// Absent ?b= + malformed ?userHash= -> daily manager anonID.
	re = requestForIdentity("/api/v1/env/dev/events?userHash=nothex", nil)
	want, err = idManager.AnonID(envID, limits.ClientIP(re), re.Request.UserAgent(), now)
	if err != nil {
		t.Fatalf("manager anonID err = %v", err)
	}
	got, _, aerr = deriveIdentity(re, envID, now)
	if aerr != nil {
		t.Fatalf("fallback path err = %v", aerr)
	}
	if got != want || got == "" {
		t.Fatalf("identity = %q, want fallback anonID %q", got, want)
	}
}

// TestFetchIngestBucketJoin is the paired join proof the package
// graph allows (fetch imports ingest, so this internal test cannot
// import fetch — cycle): the same ?b=42 canonical string fetch pins
// as ctx.UserHash (fetch.TestBucketNumberSeedEvaluation) must be
// the exact string deriveIdentity returns, so the fetch arm
// Bucket("42"+seed) and the exposure attribution join per install.
func TestFetchIngestBucketJoin(t *testing.T) {
	SetIdentityManager(identity.NewManager(identity.NewMemoryStore()))
	now := time.Now().UTC()
	const envID = "env-bucket-join"

	for _, b := range []string{"0", "42", "9999"} {
		re := requestForIdentity("/api/v1/env/dev/events?b="+b+"&userHash=092081140b677b45", nil)
		got, _, aerr := deriveIdentity(re, envID, now)
		if aerr != nil {
			t.Fatalf("b=%s: err = %v", b, aerr)
		}
		if got != b {
			t.Fatalf("b=%s: deriveIdentity = %q, want canonical %q (fetch ctx.UserHash must match)", b, got, b)
		}
	}
}
