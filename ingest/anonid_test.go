package ingest

// Bucket-only storage (migration 1790000023 renamed events.anonId to
// events.bucket): the batcher stores bucket + country. Client userHash
// values are accepted by the schema but never reach storage.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/identity"
)

// TestValidateBodyUserHashAcceptedButIgnored pins ignore+replace: a client
// userHash passes schema validation (length/NUL shape only) — rejection
// is reserved for P1 denylist keys (userId/ip/PII). The handler, not the
// schema, discards it.
func TestValidateBodyUserHashAcceptedButIgnored(t *testing.T) {
	e := ev("fetch")
	e["userHash"] = "client-supplied-opaque-value"
	events, aerr := ValidateBody(body(t, []any{e}))
	if aerr != nil {
		t.Fatalf("client userHash must be accepted by schema, got %v", aerr)
	}
	if len(events) != 1 || events[0].UserHash != "client-supplied-opaque-value" {
		t.Fatalf("userHash must decode verbatim through validation, got %+v", events)
	}
}

// TestSaveStoredEventWritesBucketOnly pins the storage contract: bucket +
// country persist and there is no userHash column, even when the client
// sent a userHash (accepted-but-ignored at validation, never stored).
func TestSaveStoredEventWritesBucketOnly(t *testing.T) {
	app := flushBenchApp(t)
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events collection: %v", err)
	}
	if col.Fields.GetByName("bucket") == nil || col.Fields.GetByName("country") == nil {
		t.Fatal("events collection must carry bucket + country (migration 1790000023)")
	}
	if col.Fields.GetByName("userHash") != nil {
		t.Fatal("events collection must not carry userHash (dropped by migration 1790000022)")
	}
	it := StoredEvent{
		EnvID: "env1", Kind: "exposure", Variant: "control",
		Bucket: "42", Country: "US",
		Ts: time.Now().UTC(), Version: 1,
	}
	if err := saveStoredEvent(app, col, it); err != nil {
		t.Fatalf("saveStoredEvent: %v", err)
	}
	recs, err := app.FindAllRecords("events")
	if err != nil || len(recs) != 1 {
		t.Fatalf("want 1 stored event, got %d (err %v)", len(recs), err)
	}
	got := recs[0]
	if got.GetString("bucket") != "42" {
		t.Errorf("bucket = %q, want bucket value", got.GetString("bucket"))
	}
	if got.GetString("country") != "US" {
		t.Errorf("country = %q, want US", got.GetString("country"))
	}
}

// TestCountryHeaderFlowsToStorage pins the country half of deriveIdentity:
// a lowercase CF-IPCountry header normalizes to ISO-2 upper case.
func TestCountryHeaderFlowsToStorage(t *testing.T) {
	h := http.Header{}
	h.Set("CF-IPCountry", "us")
	if got := identity.CountryFromHeaders(h); got != "US" {
		t.Fatalf("CountryFromHeaders(CF-IPCountry: us) = %q, want US", got)
	}
}

// Minimal RequestEvent for deriveIdentity tests.
func requestForIdentity(target string, setHeaders func(h http.Header)) *core.RequestEvent {
	req := httptest.NewRequest(http.MethodPost, target, nil)
	if setHeaders != nil {
		setHeaders(req.Header)
	}
	re := &core.RequestEvent{}
	re.Request = req
	return re
}

// Ignore+replace rule (matches fetch): any client ?userHash= or ?uid=
// shape is accepted-but-ignored — identity is the stable ?b= bucket only
// ("" when absent); a valid ?b= wins verbatim (see bucket_join_test.go).
func TestDeriveIdentityIgnoresUserHashUsesBucket(t *testing.T) {
	// Ignore path: every ?userHash= / ?uid= shape without ?b= returns "".
	for name, target := range map[string]string{
		"ignore-valid-hash": "/api/v1/env/dev/events?userHash=092081140b677b45",
		"ignore-trimmed":    "/api/v1/env/dev/events?userHash=++092081140b677b45++",
		"ignore-all-f":      "/api/v1/env/dev/events?userHash=ffffffffffffffff",
		"ignore-extra-keys": "/api/v1/env/dev/events?userHash=092081140b677b45&platform=ios",
		"absent-empty":      "/api/v1/env/dev/events?platform=ios",
		"empty-hash":        "/api/v1/env/dev/events?userHash=",
		"blank-hash":        "/api/v1/env/dev/events?userHash=+++",
		"too-short":         "/api/v1/env/dev/events?userHash=abc123",
		"too-long":          "/api/v1/env/dev/events?userHash=0123456789abcdef00",
		"uppercase":         "/api/v1/env/dev/events?userHash=0123456789ABCDEF",
		"non-hex":           "/api/v1/env/dev/events?userHash=0123456789abcdeg",
		"raw-uid-hash":      "/api/v1/env/dev/events?userHash=user-7",
		"ignore-uid":        "/api/v1/env/dev/events?uid=user-7",
		"ignore-uid-blank":  "/api/v1/env/dev/events?uid=+++",
		"ignore-both":       "/api/v1/env/dev/events?userHash=092081140b677b45&uid=user-7",
	} {
		re := requestForIdentity(target, nil)
		got, country, aerr := deriveIdentity(re)
		if aerr != nil {
			t.Errorf("%s: no ?b= must not error, got %v", name, aerr)
			continue
		}
		if got != "" {
			t.Errorf("%s: identity = %q, want empty (no ?b=, userHash/uid ignored)", name, got)
		}
		if country != "" {
			t.Errorf("%s: country = %q, want empty (no headers)", name, country)
		}
	}

	// Different ?userHash= values yield the SAME identity ("").
	first, _, aerr := deriveIdentity(requestForIdentity("/api/v1/env/dev/events?userHash=092081140b677b45", nil))
	if aerr != nil {
		t.Fatalf("same-identity setup err = %v", aerr)
	}
	second, _, aerr := deriveIdentity(requestForIdentity("/api/v1/env/dev/events?userHash=ffffffffffffffff", nil))
	if aerr != nil {
		t.Fatalf("same-identity setup err = %v", aerr)
	}
	if first != second || first != "" {
		t.Errorf("different ?userHash= yielded different identities %q vs %q (both must be empty)", first, second)
	}

	// Valid ?b= wins verbatim even alongside an ignored ?userHash=/?uid=.
	re := requestForIdentity("/api/v1/env/dev/events?b=42&userHash=092081140b677b45&uid=user-7", nil)
	got, _, aerr := deriveIdentity(re)
	if aerr != nil {
		t.Fatalf("bucket+wins err = %v", aerr)
	}
	if got != "42" {
		t.Errorf("bucket+wins = %q, want 42", got)
	}

	// Country still flows alongside an ignored ?userHash=.
	re = requestForIdentity("/api/v1/env/dev/events?userHash=092081140b677b45", func(h http.Header) {
		h.Set("CF-IPCountry", "us")
	})
	got, country, aerr := deriveIdentity(re)
	if aerr != nil {
		t.Fatalf("ignored+country err = %v", aerr)
	}
	if got != "" || country != "US" {
		t.Errorf("ignored+country = (%q, %q), want (empty, US)", got, country)
	}
}
