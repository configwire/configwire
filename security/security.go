// Package security sets static response headers with no behavior change.
// Global middleware in main.go covers every response; per-handler calls stay as backup.
//
// Headers (static values, no behavior change):
//   - X-Content-Type-Options: nosniff (block MIME sniffing)
//   - X-Frame-Options: DENY (no clickjacking host)
//   - Referrer-Policy: no-referrer (never leak URLs/keys on navigation)
//   - Content-Security-Policy: self-only scripts/styles, no plugins/frames
//
// Strict-Transport-Security is opt-in only (CONFIGWIRE_HSTS=<max-age seconds>,
// applied once at boot in main.go). The zero default keeps plain-HTTP serves
// header-free: an unconditional HSTS stamp would pin browsers to HTTPS that
// isn't there.
package security

import (
	"fmt"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// hstsMaxAge is the opt-in Strict-Transport-Security max-age in seconds.
// Zero (the default: env unset/empty/invalid) disables the header.
var hstsMaxAge int

// SetHSTSMaxAge configures the opt-in HSTS max-age (seconds). Values <= 0
// disable the header. Called once at boot; never per-request.
func SetHSTSMaxAge(seconds int) {
	if seconds <= 0 {
		hstsMaxAge = 0
		return
	}
	hstsMaxAge = seconds
}

// SetHeaders stamps static security headers. Call at the top of a handler, before any body write.
func SetHeaders(re *core.RequestEvent) {
	h := re.Response.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' https://api.github.com; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
	if hstsMaxAge > 0 {
		// includeSubDomains is a documented trade-off (plan §7 risk 2):
		// operators serving subdomains over plain HTTP should leave
		// CONFIGWIRE_HSTS unset.
		h.Set("Strict-Transport-Security", fmt.Sprintf("max-age=%d; includeSubDomains", hstsMaxAge))
	}
}
