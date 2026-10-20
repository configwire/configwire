package ingest

// anonID-only storage (migration 1790000020): the handler derives anonID
// server-side and the batcher stores anonId + country with userHash left
// empty. Client userHash values are accepted by the schema but ignored.

import (
	"net/http"
	"testing"
	"time"

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

// TestSaveStoredEventWritesAnonIDOnly pins the storage contract: anonId +
// country persist, userHash stores empty even when the StoredEvent carries
// a client value (server-derived wins).
func TestSaveStoredEventWritesAnonIDOnly(t *testing.T) {
	app := flushBenchApp(t)
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events collection: %v", err)
	}
	if col.Fields.GetByName("anonId") == nil || col.Fields.GetByName("country") == nil {
		t.Fatal("events collection must carry anonId + country (migration 1790000020)")
	}
	it := StoredEvent{
		EnvID: "env1", Kind: "exposure", Variant: "control",
		UserHash: "client-spoof-must-not-persist",
		AnonID:   "0123456789abcdef", Country: "US",
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
	if got.GetString("anonId") != "0123456789abcdef" {
		t.Errorf("anonId = %q, want server-derived value", got.GetString("anonId"))
	}
	if got.GetString("country") != "US" {
		t.Errorf("country = %q, want US", got.GetString("country"))
	}
	if got.GetString("userHash") != "" {
		t.Errorf("userHash = %q, want empty (anonID-only storage)", got.GetString("userHash"))
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
