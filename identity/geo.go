package identity

import (
	"net/http"
	"strings"
)

// countryHeaders is the backstop order (CF-IPCountry first, then the
// generic X-Country fallback for plain proxy setups). It must match
// limits.DefaultCountryHeaders — pinned by TestCountryDefaultsMatchLimits.
// Serve call sites pass the active order from the limits setting
// (Config.CountryHeaders) explicitly via CountryFromHeadersWith, so
// this package stays stdlib-only by design (see TestStdlibOnlyGeoDeps).
// Stdlib only — deliberately no MaxMind DB: country feeds eval rules
// at ISO-2 granularity, never precise geo.
var countryHeaders = []string{"CF-IPCountry", "X-Country"}

// DefaultCountryHeaders returns a fresh copy of the backstop order.
// It must match limits.DefaultCountryHeaders — pinned by
// TestCountryDefaultsMatchIdentity in the limits package (kept there
// because this package's non-test sources must stay stdlib-only and
// therefore cannot import limits).
func DefaultCountryHeaders() []string {
	out := make([]string, len(countryHeaders))
	copy(out, countryHeaders)
	return out
}

// CountryFromHeaders returns the request's ISO-2 country code from
// the first present, valid country header in the backstop order,
// else "". Serve paths prefer CountryFromHeadersWith with the active
// limits order. Values are trimmed, uppercased, and strict-checked
// (NormalizeCountry): any non-[A-Z]{2} input yields "". Provider
// unknowns that are still two letters (e.g. "XX") pass through as-is;
// rule matching treats them like any other code.
func CountryFromHeaders(h http.Header) string {
	return CountryFromHeadersWith(h, countryHeaders)
}

// CountryFromHeadersWith is the explicit-order seam behind
// CountryFromHeaders: serve call sites pass the active limits order
// here, keeping header-trust configuration out of this package.
// It returns the first present, valid ISO-2 country code, else "".
// An empty order falls back to the backstop defaults. Values are
// trimmed, uppercased, and strict-checked (NormalizeCountry): any
// non-[A-Z]{2} input yields "". Provider unknowns that are still two
// letters (e.g. "XX") pass through as-is; rule matching treats them
// like any other code.
func CountryFromHeadersWith(h http.Header, headers []string) string {
	if len(headers) == 0 {
		headers = countryHeaders
	}
	for _, name := range headers {
		if c := NormalizeCountry(h.Get(name)); c != "" {
			return c
		}
	}
	return ""
}

// NormalizeCountry trims and uppercases s, returning it only when it
// is exactly 2 ASCII letters (ISO-2); anything else returns "".
func NormalizeCountry(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 2 {
		return ""
	}
	if s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' {
		return ""
	}
	return s
}
