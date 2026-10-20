package identity

import (
	"net/http"
	"testing"
)

// TestCountryFromHeaders pins Q2 (stdlib-only, no MaxMind DB): read
// CF-IPCountry / X-Country when present, else "" — ISO-2 only.
func TestCountryFromHeaders(t *testing.T) {
	cases := []struct {
		name   string
		header map[string]string
		want   string
	}{
		{"cf lower normalized", map[string]string{"CF-IPCountry": "us"}, "US"},
		{"cf already upper", map[string]string{"CF-IPCountry": "DE"}, "DE"},
		{"x-country fallback", map[string]string{"X-Country": "jp"}, "JP"},
		{"cf wins over x-country", map[string]string{"CF-IPCountry": "FR", "X-Country": "jp"}, "FR"},
		{"absent is empty", map[string]string{}, ""},
		{"three letters rejected", map[string]string{"CF-IPCountry": "USA"}, ""},
		{"digits rejected", map[string]string{"CF-IPCountry": "1A"}, ""},
		{"blank rejected", map[string]string{"CF-IPCountry": "  "}, ""},
		{"single char rejected", map[string]string{"X-Country": "U"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.header {
				h.Set(k, v)
			}
			if got := CountryFromHeaders(h); got != tc.want {
				t.Fatalf("CountryFromHeaders = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNormalizeCountryStrictISO2: only 2 ASCII letters survive.
func TestNormalizeCountryStrictISO2(t *testing.T) {
	for _, ok := range []string{"US", "us", "Us", " gb "} {
		if got := NormalizeCountry(ok); len(got) != 2 {
			t.Fatalf("NormalizeCountry(%q) = %q, want 2-letter code", ok, got)
		}
	}
	for _, bad := range []string{"", "U", "USA", "U1", "12", "U-S", "éF"} {
		if got := NormalizeCountry(bad); got != "" {
			t.Fatalf("NormalizeCountry(%q) = %q, want empty", bad, got)
		}
	}
}
