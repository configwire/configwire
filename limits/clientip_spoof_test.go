package limits

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// TestClientIPSpoofFromPublicPeer pins the spoof boundary: headers from a
// direct public peer are ignored (peer IP wins), while the same headers
// from a loopback/private peer are honored. Covers every default header
// name plus the XFF chain shape; no network is touched.
func TestClientIPSpoofFromPublicPeer(t *testing.T) {
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })

	mk := func(pairs map[string]string) http.Header {
		hdr := http.Header{}
		for k, v := range pairs {
			hdr.Set(k, v)
		}
		return hdr
	}

	cases := []struct {
		name   string
		header map[string]string
		remote string
		want   string
	}{
		{"public ignores XFF chain", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "203.0.113.7:443", "203.0.113.7"},
		{"public ignores CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "1.1.1.1"}, "9.9.9.9:1234", "9.9.9.9"},
		{"public ignores Fly-Client-IP", map[string]string{"Fly-Client-IP": "5.5.5.5"}, "203.0.113.7:443", "203.0.113.7"},
		{"public ignores X-Real-IP", map[string]string{"X-Real-IP": "2.3.4.5"}, "9.9.9.9:1234", "9.9.9.9"},
		{"public ignores all headers at once", map[string]string{
			"CF-Connecting-IP": "1.1.1.1", "Fly-Client-IP": "5.5.5.5",
			"X-Forwarded-For": "1.2.3.4, 5.6.7.8", "X-Real-IP": "2.3.4.5",
		}, "203.0.113.7:443", "203.0.113.7"},
		{"public IPv6 ignores XFF", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "[2001:4860:4860::8888]:443", "2001:4860:4860::8888"},
		{"loopback honors XFF chain first entry", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "127.0.0.1:1234", "1.2.3.4"},
		{"loopback honors CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "1.1.1.1"}, "127.0.0.1:1234", "1.1.1.1"},
		{"loopback honors Fly-Client-IP", map[string]string{"Fly-Client-IP": "5.5.5.5"}, "127.0.0.1:1234", "5.5.5.5"},
		{"loopback honors X-Real-IP", map[string]string{"X-Real-IP": "2.3.4.5"}, "127.0.0.1:1234", "2.3.4.5"},
		{"private 10.x honors XFF", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "10.0.0.5:1234", "1.2.3.4"},
		{"private 192.168 honors CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "1.1.1.1"}, "192.168.1.7:8090", "1.1.1.1"},
	}

	for _, tc := range cases {
		if got := clientIPFromHeaders(mk(tc.header), tc.remote); got != tc.want {
			t.Errorf("%s: clientIPFromHeaders = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestClientIPSpoofEndToEnd pins the same boundary through the exported
// ClientIP wrapper (httptest request + RequestEvent, no network): a public
// peer's spoofed headers lose, a loopback peer's identical headers win.
func TestClientIPSpoofEndToEnd(t *testing.T) {
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })

	newEvent := func(remote string, headers map[string]string) *core.RequestEvent {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/env/dev/config", nil)
		req.RemoteAddr = remote
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		re := &core.RequestEvent{}
		re.Request = req
		return re
	}

	spoof := map[string]string{
		"CF-Connecting-IP": "1.1.1.1",
		"X-Forwarded-For":  "1.2.3.4, 5.6.7.8",
	}

	if got := ClientIP(newEvent("203.0.113.7:443", spoof)); got != "203.0.113.7" {
		t.Fatalf("public peer ClientIP = %q, want peer IP (spoof ignored)", got)
	}
	if got := ClientIP(newEvent("127.0.0.1:1234", spoof)); got != "1.1.1.1" {
		t.Fatalf("loopback peer ClientIP = %q, want header IP 1.1.1.1", got)
	}
	if got := ClientIP(newEvent("10.0.0.5:1234", map[string]string{"X-Forwarded-For": "1.2.3.4"})); got != "1.2.3.4" {
		t.Fatalf("private peer ClientIP = %q, want header IP 1.2.3.4", got)
	}
}
