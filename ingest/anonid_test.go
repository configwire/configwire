package ingest

// anonID-only storage (migration 1790000020): the handler derives anonID
// server-side and the batcher stores anonId + country with userHash left
// empty. Client userHash values are accepted by the schema but ignored.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/configwire/configwire/identity"
	"github.com/configwire/configwire/limits"
	"github.com/pocketbase/pocketbase/core"
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

// Same honored-hash rule as fetch; vectors mirror TestEffectiveUserHash.
func TestDeriveIdentityHonorsValidHashElseAnonID(t *testing.T) {
	SetIdentityManager(identity.NewManager(identity.NewMemoryStore()))
	now := time.Now().UTC()
	const envID = "env-parity"

	// Honor path: well-formed hashes return verbatim (no derivation).
	for name, tc := range map[string]struct {
		target string
		want   string
	}{
		"honor-valid-hash": {"/api/v1/env/dev/events?userHash=092081140b677b45", "092081140b677b45"},
		"honor-trimmed":    {"/api/v1/env/dev/events?userHash=++092081140b677b45++", "092081140b677b45"},
		"honor-all-f":      {"/api/v1/env/dev/events?userHash=ffffffffffffffff", "ffffffffffffffff"},
		"honor-extra-keys": {"/api/v1/env/dev/events?userHash=092081140b677b45&platform=ios", "092081140b677b45"},
	} {
		re := requestForIdentity(tc.target, nil)
		got, country, aerr := deriveIdentity(re, envID, now)
		if aerr != nil {
			t.Errorf("%s: no ?b= must not error, got %v", name, aerr)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: identity = %q, want honored hash %q", name, got, tc.want)
		}
		if country != "" {
			t.Errorf("%s: country = %q, want empty (no headers)", name, country)
		}
	}

	// Fallback path: absent/malformed returns the daily manager anonID,
	// identical to the value fetch would bucket on for the same request.
	for name, target := range map[string]string{
		"absent-fallback": "/api/v1/env/dev/events?platform=ios",
		"empty-fallback":  "/api/v1/env/dev/events?userHash=",
		"blank-fallback":  "/api/v1/env/dev/events?userHash=+++",
		"too-short":       "/api/v1/env/dev/events?userHash=abc123",
		"too-long":        "/api/v1/env/dev/events?userHash=0123456789abcdef00",
		"uppercase":       "/api/v1/env/dev/events?userHash=0123456789ABCDEF",
		"non-hex":         "/api/v1/env/dev/events?userHash=0123456789abcdeg",
		"raw-uid":         "/api/v1/env/dev/events?userHash=user-7",
	} {
		re := requestForIdentity(target, nil)
		want, err := idManager.AnonID(envID, limits.ClientIP(re), re.Request.UserAgent(), now)
		if err != nil {
			t.Fatalf("%s: manager anonID err = %v", name, err)
		}
		got, _, aerr := deriveIdentity(re, envID, now)
		if aerr != nil {
			t.Errorf("%s: no ?b= must not error, got %v", name, aerr)
			continue
		}
		if got != want || got == "" {
			t.Errorf("%s: identity = %q, want fallback anonID %q", name, got, want)
		}
		if honored := identity.ResolveHonoredHash(re.Request.URL.Query().Get("userHash"), want); honored != want {
			t.Errorf("%s: fetch-side rule = %q, want same fallback %q", name, honored, want)
		}
	}

	// Country still flows on the honored path.
	re := requestForIdentity("/api/v1/env/dev/events?userHash=092081140b677b45", func(h http.Header) {
		h.Set("CF-IPCountry", "us")
	})
	got, country, aerr := deriveIdentity(re, envID, now)
	if aerr != nil {
		t.Fatalf("honored+country err = %v", aerr)
	}
	if got != "092081140b677b45" || country != "US" {
		t.Errorf("honored+country = (%q, %q), want (hash, US)", got, country)
	}
}
