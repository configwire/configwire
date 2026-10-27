package security

import (
	"net/http/httptest"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// headerRecorder builds a RequestEvent with a real ResponseWriter so the
// stamped headers can be asserted instead of panicking on a nil Response.
func headerRecorder() (*core.RequestEvent, *httptest.ResponseRecorder) {
	re := &core.RequestEvent{}
	rec := httptest.NewRecorder()
	re.Response = rec
	return re, rec
}

func TestSetHeadersHSTS(t *testing.T) {
	// Boot mapping (enforced by parseHSTSMaxAge/applyHSTSConfig in main.go):
	// only a positive decimal CONFIGWIRE_HSTS ever reaches SetHSTSMaxAge.
	// Unset/empty/bogus leave the package var at its zero default, which the
	// "zero disables" and "default off" cases below pin down.
	tests := []struct {
		name   string
		maxAge int
		want   string // "" means the header must be absent
	}{
		{"positive stamps exact value", 31536000, "max-age=31536000; includeSubDomains"},
		{"one second", 1, "max-age=1; includeSubDomains"},
		{"zero disables", 0, ""},
		{"negative clamps to disabled", -5, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetHSTSMaxAge(tc.maxAge)
			t.Cleanup(func() { SetHSTSMaxAge(0) })
			re, rec := headerRecorder()
			SetHeaders(re)
			if got := rec.Header().Get("Strict-Transport-Security"); got != tc.want {
				t.Fatalf("Strict-Transport-Security = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetHeadersHSTSDefaultOff(t *testing.T) {
	// Zero-env boot: the setter is never called, so no HSTS header may appear
	// on any response — existing behavior byte-identical.
	SetHSTSMaxAge(0) // clean slate even if tests reorder
	re, rec := headerRecorder()
	SetHeaders(re)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("default boot must not stamp HSTS, got %q", got)
	}
	// The static headers are untouched by the opt-in knob.
	wantStatic := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, want := range wantStatic {
		if got := rec.Header().Get(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatalf("Content-Security-Policy must still be stamped by default")
	}
}
