package identity

import (
	"net/http"
	"strings"
)

// Country header names, checked in order. Cloudflare sets
// CF-IPCountry on every request through its proxy; X-Country is the
// generic back-compat fallback for plain proxy setups. Stdlib only —
// deliberately no MaxMind DB (Q2 locked): country feeds eval rules at
// ISO-2 granularity, never precise geo.
var countryHeaders = []string{"CF-IPCountry", "X-Country"}

// CountryFromHeaders returns the request's ISO-2 country code from
// the first present, valid country header, else "". Values are
// trimmed, uppercased, and strict-checked (NormalizeCountry): any
// non-[A-Z]{2} input yields "". Provider unknowns that are still two
// letters (e.g. Cloudflare "XX") pass through as-is; rule matching
// treats them like any other code.
func CountryFromHeaders(h http.Header) string {
	for _, name := range countryHeaders {
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
