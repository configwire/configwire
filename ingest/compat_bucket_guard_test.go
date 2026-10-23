package ingest

// T8 backward-compat regression guards for the events path in the bucket
// era. Additive only — no existing test assertion is touched here.
//
// Already pinned elsewhere (referenced, not duplicated):
//   - body userHash accepted-but-ignored by schema: TestValidateBodyUserHashAcceptedButIgnored
//   - anonID-only storage (userHash column stays empty): TestSaveStoredEventWritesAnonIDOnly
//   - country header normalization without ?b=: TestCountryHeaderFlowsToStorage
//   - no-?b= ignored-hash and anonID fallback: TestDeriveIdentityIgnoresUserHashUsesAnonID
//   - ?b= honored / malformed-400 / absent-fallback: TestDeriveIdentityBucketHonored,
//     TestDeriveIdentityBucketMalformedIs400, TestDeriveIdentityBucketAbsentFallsBack
//   - fetch/ingest ?b= join: TestFetchIngestBucketJoin

import (
	"net/http"
	"testing"
	"time"

	"github.com/configwire/configwire/identity"
)

// TestCompatBucketCountryFlowsToStorage pins that country derivation is
// unchanged when ?b= IS present: the CF-IPCountry header still flows
// verbatim alongside the bucket identity, and a missing header still
// yields "" — the bucket branch never touches country.
func TestCompatBucketCountryFlowsToStorage(t *testing.T) {
	SetIdentityManager(identity.NewManager(identity.NewMemoryStore()))
	now := time.Now().UTC()
	const envID = "env-compat-bucket-country"

	re := requestForIdentity("/api/v1/env/dev/events?b=42", func(h http.Header) {
		h.Set("CF-IPCountry", "us")
	})
	got, country, aerr := deriveIdentity(re, envID, now)
	if aerr != nil {
		t.Fatalf("bucket+country err = %v", aerr)
	}
	if got != "42" || country != "US" {
		t.Fatalf("bucket+country = (%q, %q), want (42, US)", got, country)
	}

	re = requestForIdentity("/api/v1/env/dev/events?b=9999", nil)
	got, country, aerr = deriveIdentity(re, envID, now)
	if aerr != nil {
		t.Fatalf("bucket without headers err = %v", aerr)
	}
	if got != "9999" || country != "" {
		t.Fatalf("bucket bare = (%q, %q), want (9999, empty)", got, country)
	}
}

// TestCompatBodyUserHashAcceptedAlongsideBucket pins that the schema layer
// is untouched by the bucket era: an event body carrying userHash still
// validates (accepted-but-ignored) on a request whose identity comes from
// ?b= — the query identity and the body contract stay independent.
func TestCompatBodyUserHashAcceptedAlongsideBucket(t *testing.T) {
	e := ev("fetch")
	e["userHash"] = "client-spoof-must-not-persist"
	e["flag"] = "new_ui"
	e["variant"] = "treatment"
	events, aerr := ValidateBody(body(t, []any{e}))
	if aerr != nil {
		t.Fatalf("body userHash alongside bucket-era query must validate, got %v", aerr)
	}
	if len(events) != 1 || events[0].UserHash != "client-spoof-must-not-persist" {
		t.Fatalf("body userHash must decode verbatim, got %+v", events)
	}
}
