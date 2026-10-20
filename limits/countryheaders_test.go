package limits

import (
	"strings"
	"testing"
)

func TestDefaultCountryHeadersOrder(t *testing.T) {
	want := []string{"CF-IPCountry", "X-Country"}
	got := DefaultCountryHeaders()
	if len(got) != len(want) {
		t.Fatalf("DefaultCountryHeaders() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DefaultCountryHeaders() = %q, want %q", got, want)
		}
	}
	if d := Defaults(); len(d.CountryHeaders) != len(want) {
		t.Fatalf("Defaults().CountryHeaders = %q, want %q", d.CountryHeaders, want)
	}
	// Fresh copy: mutating the result must not poison the canonical list.
	got[0] = "MUTATED"
	if again := DefaultCountryHeaders(); again[0] != want[0] {
		t.Fatalf("DefaultCountryHeaders() shares backing array: %q", again)
	}
}

func TestValidateCountryHeaders(t *testing.T) {
	valid := []struct {
		name string
		in   []string
	}{
		{"nil means defaults", nil},
		{"empty means defaults", []string{}},
		{"all-blank means defaults", []string{"", "   "}},
		{"defaults", DefaultCountryHeaders()},
		{"single header", []string{"CF-IPCountry"}},
		{"ten entries", []string{"H-1", "H-2", "H-3", "H-4", "H-5", "H-6", "H-7", "H-8", "H-9", "H-10"}},
		{"dedupe folds to one", []string{"X-Country", "x-country", " X-COUNTRY "}},
		{"64-char name", []string{strings.Repeat("a", 64)}},
	}
	for _, tc := range valid {
		if err := ValidateCountryHeaders(tc.in); err != nil {
			t.Errorf("%s: ValidateCountryHeaders(%q) = %v, want nil", tc.name, tc.in, err)
		}
		if err := ValidateConfig(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil, CountryHeaders: tc.in}); err != nil {
			t.Errorf("%s: ValidateConfig = %v, want nil", tc.name, err)
		}
	}

	eleven := []string{"H-1", "H-2", "H-3", "H-4", "H-5", "H-6", "H-7", "H-8", "H-9", "H-10", "H-11"}
	invalid := []struct {
		name string
		in   []string
	}{
		{"eleven entries", eleven},
		{"space inside", []string{"X Country"}},
		{"underscore", []string{"X_Country"}},
		{"comma", []string{"a,b"}},
		{"colon", []string{"X-Country:1"}},
		{"65-char name", []string{strings.Repeat("a", 65)}},
		{"unicode", []string{"X-Cöuntry"}},
	}
	for _, tc := range invalid {
		if err := ValidateCountryHeaders(tc.in); err == nil {
			t.Errorf("%s: ValidateCountryHeaders(%q) = nil, want error", tc.name, tc.in)
		}
		bad := Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminAllowedIPs: nil, CountryHeaders: tc.in}
		if err := ValidateConfig(bad); err == nil {
			t.Errorf("%s: ValidateConfig = nil, want error", tc.name)
		}
	}
}

func TestNormalizeCountryHeaders(t *testing.T) {
	got := normalizeCountryHeaders([]string{" X-Country ", "CF-IPCountry", "x-country", "", "  "})
	want := []string{"X-Country", "CF-IPCountry"}
	if len(got) != len(want) {
		t.Fatalf("normalizeCountryHeaders = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeCountryHeaders = %q, want %q", got, want)
		}
	}
	if got := normalizeCountryHeaders(nil); len(got) != 0 {
		t.Fatalf("normalizeCountryHeaders(nil) = %q, want empty", got)
	}
	many := []string{"H-1", "H-2", "H-3", "H-4", "H-5", "H-6", "H-7", "H-8", "H-9", "H-10", "H-11", "H-12"}
	if got := normalizeCountryHeaders(many); len(got) != maxIPHeaders {
		t.Fatalf("normalizeCountryHeaders caps at %d, got %q", maxIPHeaders, got)
	}
}

func TestParseCountryHeadersEnv(t *testing.T) {
	t.Setenv(countryHeadersEnvName, "")
	if got := loadCountryHeadersFromEnv(); strings.Join(got, ",") != strings.Join(DefaultCountryHeaders(), ",") {
		t.Fatalf("empty env = %q, want defaults", got)
	}
	t.Setenv(countryHeadersEnvName, "X-Country, CF-IPCountry")
	if got := loadCountryHeadersFromEnv(); strings.Join(got, ",") != "X-Country,CF-IPCountry" {
		t.Fatalf("custom env = %q", got)
	}
	t.Setenv(countryHeadersEnvName, "bogus header!, ,CF-IPCountry")
	if got := loadCountryHeadersFromEnv(); len(got) != 1 || got[0] != "CF-IPCountry" {
		t.Fatalf("invalid env names should be dropped, got %q", got)
	}
}

func TestResolvePUTCountryHeaders(t *testing.T) {
	stored := []string{"X-Country"}
	if got := resolvePUTCountryHeaders(stored, nil); strings.Join(got, ",") != "X-Country" {
		t.Fatalf("nil keeps stored order, got %q", got)
	}
	if got := resolvePUTCountryHeaders(stored, &[]string{}); strings.Join(got, ",") != strings.Join(DefaultCountryHeaders(), ",") {
		t.Fatalf("empty resets to defaults, got %q", got)
	}
	blank := []string{"  ", ""}
	if got := resolvePUTCountryHeaders(stored, &blank); strings.Join(got, ",") != strings.Join(DefaultCountryHeaders(), ",") {
		t.Fatalf("all-blank resets to defaults, got %q", got)
	}
	custom := []string{" X-Country ", "X-Country", "CF-IPCountry"}
	if got := resolvePUTCountryHeaders(stored, &custom); strings.Join(got, ",") != "X-Country,CF-IPCountry" {
		t.Fatalf("present list normalizes, got %q", got)
	}
	// Keeping stored must copy: mutating the result leaves globals alone.
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })
	kept := resolvePUTCountryHeaders(GetConfig().CountryHeaders, nil)
	kept[0] = "MUTATED"
	if again := GetConfig().CountryHeaders; again[0] == "MUTATED" {
		t.Fatal("resolvePUTCountryHeaders aliases the stored order")
	}
}

func TestGetConfigCopiesCountryHeaders(t *testing.T) {
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })
	got := GetConfig()
	got.CountryHeaders[0] = "MUTATED"
	if again := GetConfig(); again.CountryHeaders[0] == "MUTATED" {
		t.Fatal("GetConfig aliases the active country header order")
	}
}
