package updatecheck

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// updateEvent builds a bare RequestEvent over a recorder for direct
// handler calls (only Request/Response are touched).
func updateEvent() (*core.RequestEvent, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/update-check", nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{}
	re.Request = req
	re.Response = rec
	return re, rec
}

// withEndpoint points the fetch target at srv for the test and restores
// the production const default afterwards.
func withEndpoint(t *testing.T, url string) {
	t.Helper()
	old := releasesEndpoint
	releasesEndpoint = url
	resetCache()
	t.Cleanup(func() { releasesEndpoint = old })
}

// withVersion sets the injected binary version and restores it afterwards.
func withVersion(t *testing.T, v string) {
	t.Helper()
	old := Version
	Version = v
	resetCache()
	t.Cleanup(func() { Version = old })
}

// decodeBody unwraps the recorder JSON into a string-keyed map.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestGateOffAnswers403WithoutNetwork(t *testing.T) {
	// Disabled values must short-circuit before any outbound fetch: the
	// httptest server counts hits and must stay at zero.
	for _, env := range []string{"", "0", "true", "yes", " 1", "2"} {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv("CONFIGWIRE_CHECK_UPDATES", env)
			var hits atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)
			withEndpoint(t, srv.URL)
			withVersion(t, "v0.1.0")

			re, rec := updateEvent()
			if err := handleUpdateCheck(re); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			body := decodeBody(t, rec)
			if body["message"] != "update checks disabled" {
				t.Fatalf("message = %v, want %q", body["message"], "update checks disabled")
			}
			if body["status"] != float64(http.StatusForbidden) {
				t.Fatalf("status field = %v, want 403", body["status"])
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("disabled gate made %d outbound request(s), want 0", n)
			}
		})
	}
}

func TestGateOffUnsetAnswers403WithoutNetwork(t *testing.T) {
	// Unset (not just empty) must also short-circuit.
	os.Unsetenv("CONFIGWIRE_CHECK_UPDATES")
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)
	withVersion(t, "v0.1.0")

	re, rec := updateEvent()
	if err := handleUpdateCheck(re); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("disabled gate made %d outbound request(s), want 0", n)
	}
}

func TestEnabled(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want bool
	}{
		{"1", true},
		{"", false},
		{"0", false},
		{"true", false},
		{" 1", false},
	} {
		t.Run("env="+tc.env, func(t *testing.T) {
			t.Setenv("CONFIGWIRE_CHECK_UPDATES", tc.env)
			if got := Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsDevTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"dev", true},
		{"vdev", true},
		{"Dev", true},
		{"VDEV", true},
		{"  vdev  ", true},
		{"v0.1.0", false},
		{"0.2.0-rc.11", false},
		{"development", false},
	}
	for _, tc := range tests {
		if got := IsDevTag(tc.tag); got != tc.want {
			t.Errorf("IsDevTag(%q) = %v, want %v", tc.tag, got, tc.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "vdev"},
		{"   ", "vdev"},
		{"0.2.0", "v0.2.0"},
		{"v0.2.0", "v0.2.0"},
		{"  v1.2.3-rc.1  ", "v1.2.3-rc.1"},
		{"vdev", "vdev"},
	}
	for _, tc := range tests {
		if got := NormalizeVersion(tc.in); got != tc.want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"equal stable", "v1.2.3", "v1.2.3", false},
		{"patch bump", "v1.2.4", "v1.2.3", true},
		{"patch older", "v1.2.3", "v1.2.4", false},
		{"stable beats same-core pre", "v1.2.3", "v1.2.3-rc.1", true},
		{"pre loses to same-core stable", "v1.2.3-rc.1", "v1.2.3", false},
		{"numeric pre 12 beats 11", "v0.2.0-rc.12", "v0.2.0-rc.11", true},
		{"numeric pre 11 loses to 12", "v0.2.0-rc.11", "v0.2.0-rc.12", false},
		{"longer core wins prefix tie", "v1.2.1", "v1.2", true},
		{"shorter core loses prefix tie", "v1.2", "v1.2.1", false},
		{"longer pre wins tie", "v1.2.3-rc.1.1", "v1.2.3-rc.1", true},
		{"lexical pre", "v1.0.0-beta", "v1.0.0-alpha", true},
		{"numeric pre below alphanumeric", "v1.0.0-rc.x", "v1.0.0-rc.1", true},
		{"alphanumeric pre above numeric", "v1.0.0-rc.1", "v1.0.0-rc.x", false},
		{"non-numeric core counts as zero", "v2.0", "vX.9", true},
		{"major beats minor", "v2.0.0", "v1.9.9", true},
		{"upper V prefix stripped", "V1.2.4", "v1.2.3", true},
		{"missing prefix tolerated", "1.2.4", "v1.2.3", true},
		{"whitespace tolerated", "  v1.2.4  ", "v1.2.3", true},
		{"major pre-release across minors", "v0.2.0-rc.1", "v0.1.9", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNewer(tc.a, tc.b); got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestBestTag(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{"picks newest stable", []string{"v0.2.0-rc.11", "v0.2.0", "v0.1.9"}, "v0.2.0"},
		{"skips dev placeholders", []string{"", "dev", "vdev", " VDEV ", "v0.1.0"}, "v0.1.0"},
		{"trims whitespace", []string{"  v0.1.0  "}, "v0.1.0"},
		{"none usable", []string{"", "dev", "vdev"}, ""},
		{"empty input", nil, ""},
		{"newest pre when no stable", []string{"v0.2.0-rc.11", "v0.2.0-rc.12"}, "v0.2.0-rc.12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BestTag(tc.tags); got != tc.want {
				t.Errorf("BestTag(%q) = %q, want %q", tc.tags, got, tc.want)
			}
		})
	}
}

func TestHandlerSuccess(t *testing.T) {
	t.Setenv("CONFIGWIRE_CHECK_UPDATES", "1")
	var gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v0.2.0-rc.11"},{"tag_name":"v0.2.0"},{"tag_name":"dev"},{"tag_name":""}]`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)
	withVersion(t, "v0.1.0")

	re, rec := updateEvent()
	if err := handleUpdateCheck(re); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["current"] != "v0.1.0" {
		t.Errorf("current = %v, want %q", body["current"], "v0.1.0")
	}
	if body["latest"] != "v0.2.0" {
		t.Errorf("latest = %v, want %q", body["latest"], "v0.2.0")
	}
	if body["update"] != true {
		t.Errorf("update = %v, want true", body["update"])
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept header = %q, want %q", gotAccept, "application/vnd.github+json")
	}
	if gotUA != "configwire-update-check" {
		t.Errorf("User-Agent header = %q, want %q", gotUA, "configwire-update-check")
	}
}

func TestHandlerNoUpdateWhenCurrentNewer(t *testing.T) {
	t.Setenv("CONFIGWIRE_CHECK_UPDATES", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name":"v0.1.0"}]`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)
	withVersion(t, "v0.2.0")

	re, rec := updateEvent()
	if err := handleUpdateCheck(re); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["latest"] != "v0.1.0" {
		t.Errorf("latest = %v, want %q", body["latest"], "v0.1.0")
	}
	if body["update"] != false {
		t.Errorf("update = %v, want false (current is newer)", body["update"])
	}
}

func TestHandlerDevCurrentNeverUpdates(t *testing.T) {
	// A dev binary must never report an update, even when releases exist.
	t.Setenv("CONFIGWIRE_CHECK_UPDATES", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name":"v9.9.9"}]`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)
	for _, v := range []string{"vdev", "dev", ""} {
		withVersion(t, v)
		re, rec := updateEvent()
		if err := handleUpdateCheck(re); err != nil {
			t.Fatalf("version %q: handler returned error: %v", v, err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("version %q: status = %d, want 200", v, rec.Code)
		}
		body := decodeBody(t, rec)
		if body["update"] != false {
			t.Errorf("version %q: update = %v, want false", v, body["update"])
		}
	}
}

func TestHandlerEmptyTags(t *testing.T) {
	t.Setenv("CONFIGWIRE_CHECK_UPDATES", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name":"dev"},{}]`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)
	withVersion(t, "v0.1.0")

	re, rec := updateEvent()
	if err := handleUpdateCheck(re); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["latest"] != "" {
		t.Errorf("latest = %v, want empty", body["latest"])
	}
	if body["update"] != false {
		t.Errorf("update = %v, want false", body["update"])
	}
}

func TestHandlerCachesSuccess(t *testing.T) {
	t.Setenv("CONFIGWIRE_CHECK_UPDATES", "1")
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`[{"tag_name":"v0.2.0"}]`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)
	withVersion(t, "v0.1.0")

	for i := 0; i < 2; i++ {
		re, rec := updateEvent()
		if err := handleUpdateCheck(re); err != nil {
			t.Fatalf("call %d: handler returned error: %v", i, err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200", i, rec.Code)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("two checks made %d upstream requests, want 1 (second served from cache)", n)
	}

	resetCache()
	re, rec := updateEvent()
	if err := handleUpdateCheck(re); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("post-reset check made %d total upstream requests, want 2", n)
	}
}

func TestHandlerFailSoft(t *testing.T) {
	t.Setenv("CONFIGWIRE_CHECK_UPDATES", "1")
	withVersion(t, "v0.1.0")
	cases := map[string]http.HandlerFunc{
		"upstream 500": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"upstream 403": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
		"invalid json": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`not json`))
		},
		"wrong shape": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v9.9.9"}`))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			withEndpoint(t, srv.URL)

			re, rec := updateEvent()
			if err := handleUpdateCheck(re); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502 (body %q)", rec.Code, rec.Body.String())
			}
			body := decodeBody(t, rec)
			if body["message"] != "update check failed" {
				t.Errorf("message = %v, want %q", body["message"], "update check failed")
			}
			if body["status"] != float64(http.StatusBadGateway) {
				t.Errorf("status field = %v, want 502", body["status"])
			}
		})
	}
}
