package limits

import (
	"net/http"
	"strings"
	"testing"
)

func TestDefaultIPHeadersOrder(t *testing.T) {
	want := []string{"CF-Connecting-IP", "Fly-Client-IP", "X-Forwarded-For", "X-Real-IP"}
	got := DefaultIPHeaders()
	if len(got) != len(want) {
		t.Fatalf("DefaultIPHeaders() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DefaultIPHeaders() = %q, want %q", got, want)
		}
	}
	if d := Defaults(); len(d.IPHeaders) != len(want) {
		t.Fatalf("Defaults().IPHeaders = %q, want %q", d.IPHeaders, want)
	}
	// Fresh copy: mutating the result must not poison the canonical list.
	got[0] = "MUTATED"
	if again := DefaultIPHeaders(); again[0] != want[0] {
		t.Fatalf("DefaultIPHeaders() shares backing array: %q", again)
	}
}

func TestValidateIPHeaders(t *testing.T) {
	valid := []struct {
		name string
		in   []string
	}{
		{"nil means defaults", nil},
		{"empty means defaults", []string{}},
		{"all-blank means defaults", []string{"", "   "}},
		{"defaults", DefaultIPHeaders()},
		{"single CDN header", []string{"CF-Connecting-IP"}},
		{"ten entries", []string{"H-1", "H-2", "H-3", "H-4", "H-5", "H-6", "H-7", "H-8", "H-9", "H-10"}},
		{"dedupe folds to one", []string{"X-Forwarded-For", "x-forwarded-for", " X-FORWARDED-FOR "}},
		{"64-char name", []string{strings.Repeat("a", 64)}},
	}
	for _, tc := range valid {
		if err := ValidateIPHeaders(tc.in); err != nil {
			t.Errorf("%s: ValidateIPHeaders(%q) = %v, want nil", tc.name, tc.in, err)
		}
		if err := ValidateConfig(Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 20, IPHeaders: tc.in}); err != nil {
			t.Errorf("%s: ValidateConfig = %v, want nil", tc.name, err)
		}
	}

	eleven := []string{"H-1", "H-2", "H-3", "H-4", "H-5", "H-6", "H-7", "H-8", "H-9", "H-10", "H-11"}
	invalid := []struct {
		name string
		in   []string
	}{
		{"eleven entries", eleven},
		{"space inside", []string{"X Forwarded For"}},
		{"underscore", []string{"X_Forwarded_For"}},
		{"comma", []string{"a,b"}},
		{"colon", []string{"X-Real-IP:1"}},
		{"65-char name", []string{strings.Repeat("a", 65)}},
		{"unicode", []string{"X-Förwarded"}},
	}
	for _, tc := range invalid {
		if err := ValidateIPHeaders(tc.in); err == nil {
			t.Errorf("%s: ValidateIPHeaders(%q) = nil, want error", tc.name, tc.in)
		}
		bad := Config{GlobalRps: 200, Burst: 400, FetchRps: 100, IngestRps: 50, AdminRps: 20, IPHeaders: tc.in}
		if err := ValidateConfig(bad); err == nil {
			t.Errorf("%s: ValidateConfig = nil, want error", tc.name)
		}
	}
}

func TestNormalizeIPHeaders(t *testing.T) {
	got := normalizeIPHeaders([]string{" Fly-Client-IP ", "CF-Connecting-IP", "fly-client-ip", "", "  ", "X-Forwarded-For"})
	want := []string{"Fly-Client-IP", "CF-Connecting-IP", "X-Forwarded-For"}
	if len(got) != len(want) {
		t.Fatalf("normalizeIPHeaders = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeIPHeaders = %q, want %q", got, want)
		}
	}
	if got := normalizeIPHeaders(nil); len(got) != 0 {
		t.Fatalf("normalizeIPHeaders(nil) = %q, want empty", got)
	}
	many := []string{"H-1", "H-2", "H-3", "H-4", "H-5", "H-6", "H-7", "H-8", "H-9", "H-10", "H-11", "H-12"}
	if got := normalizeIPHeaders(many); len(got) != maxIPHeaders {
		t.Fatalf("normalizeIPHeaders caps at %d, got %q", maxIPHeaders, got)
	}
}

func TestParseIPHeadersEnv(t *testing.T) {
	t.Setenv(ipHeadersEnvName, "")
	if got := loadIPHeadersFromEnv(); strings.Join(got, ",") != strings.Join(DefaultIPHeaders(), ",") {
		t.Fatalf("empty env = %q, want defaults", got)
	}
	t.Setenv(ipHeadersEnvName, "Fly-Client-IP, X-Forwarded-For")
	if got := loadIPHeadersFromEnv(); strings.Join(got, ",") != "Fly-Client-IP,X-Forwarded-For" {
		t.Fatalf("custom env = %q", got)
	}
	t.Setenv(ipHeadersEnvName, "bogus header!, ,CF-Connecting-IP")
	if got := loadIPHeadersFromEnv(); len(got) != 1 || got[0] != "CF-Connecting-IP" {
		t.Fatalf("invalid env names should be dropped, got %q", got)
	}
}

func TestClientIPPrecedenceWithOrder(t *testing.T) {
	mk := func(pairs map[string]string) http.Header {
		hdr := http.Header{}
		for k, v := range pairs {
			hdr.Set(k, v)
		}
		return hdr
	}
	cfFirst := []string{"CF-Connecting-IP", "Fly-Client-IP", "X-Forwarded-For", "X-Real-IP"}
	cases := []struct {
		name    string
		headers []string
		header  map[string]string
		remote  string
		want    string
	}{
		{
			"CF wins over XFF when both present",
			cfFirst,
			map[string]string{"CF-Connecting-IP": "1.1.1.1", "X-Forwarded-For": "2.2.2.2, 3.3.3.3"},
			"127.0.0.1:1234", "1.1.1.1",
		},
		{
			"falls back to XFF chain first entry when CF absent",
			cfFirst,
			map[string]string{"X-Forwarded-For": "2.2.2.2, 3.3.3.3", "X-Real-IP": "4.4.4.4"},
			"127.0.0.1:1234", "2.2.2.2",
		},
		{
			"custom order prefers X-Real-IP over XFF",
			[]string{"X-Real-IP", "X-Forwarded-For"},
			map[string]string{"X-Forwarded-For": "2.2.2.2", "X-Real-IP": "4.4.4.4"},
			"10.0.0.5:1234", "4.4.4.4",
		},
		{
			"invalid first header falls through to next valid",
			cfFirst,
			map[string]string{"CF-Connecting-IP": "garbage", "X-Forwarded-For": "2.2.2.2"},
			"127.0.0.1:1234", "2.2.2.2",
		},
		{
			"all headers invalid falls back to trusted remote",
			cfFirst,
			map[string]string{"CF-Connecting-IP": "garbage"},
			"127.0.0.1:1234", "127.0.0.1",
		},
		{
			"Fly single-value header honored",
			cfFirst,
			map[string]string{"Fly-Client-IP": "5.5.5.5"},
			"10.0.0.5:8080", "5.5.5.5",
		},
		{
			"config name case does not matter",
			[]string{"cf-connecting-ip"},
			map[string]string{"CF-Connecting-IP": "1.1.1.1"},
			"127.0.0.1:1234", "1.1.1.1",
		},
		{
			"public peer ignores every configured header",
			cfFirst,
			map[string]string{"CF-Connecting-IP": "1.1.1.1", "X-Forwarded-For": "2.2.2.2"},
			"9.9.9.9:1234", "9.9.9.9",
		},
		{
			"public peer ignores spoofed Fly header too",
			cfFirst,
			map[string]string{"Fly-Client-IP": "5.5.5.5"},
			"203.0.113.7:443", "203.0.113.7",
		},
		{
			"empty remote preserves header-first behavior",
			cfFirst,
			map[string]string{"CF-Connecting-IP": "1.1.1.1"},
			"", "1.1.1.1",
		},
		{
			"empty order falls back to defaults",
			nil,
			map[string]string{"X-Forwarded-For": "2.2.2.2"},
			"127.0.0.1:1234", "2.2.2.2",
		},
	}
	for _, tc := range cases {
		if got := clientIPFromHeadersWith(mk(tc.header), tc.remote, tc.headers); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestResolvePUTHeaders(t *testing.T) {
	stored := []string{"Fly-Client-IP"}
	if got := resolvePUTHeaders(stored, nil); strings.Join(got, ",") != "Fly-Client-IP" {
		t.Fatalf("nil keeps stored order, got %q", got)
	}
	if got := resolvePUTHeaders(stored, &[]string{}); strings.Join(got, ",") != strings.Join(DefaultIPHeaders(), ",") {
		t.Fatalf("empty resets to defaults, got %q", got)
	}
	blank := []string{"  ", ""}
	if got := resolvePUTHeaders(stored, &blank); strings.Join(got, ",") != strings.Join(DefaultIPHeaders(), ",") {
		t.Fatalf("all-blank resets to defaults, got %q", got)
	}
	custom := []string{" X-Real-IP ", "X-Real-IP", "X-Forwarded-For"}
	if got := resolvePUTHeaders(stored, &custom); strings.Join(got, ",") != "X-Real-IP,X-Forwarded-For" {
		t.Fatalf("present list normalizes, got %q", got)
	}
	// Keeping stored must copy: mutating the result leaves globals alone.
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })
	kept := resolvePUTHeaders(GetConfig().IPHeaders, nil)
	kept[0] = "MUTATED"
	if again := GetConfig().IPHeaders; again[0] == "MUTATED" {
		t.Fatal("resolvePUTHeaders aliases the stored order")
	}
}

func TestGetConfigCopiesHeaders(t *testing.T) {
	resetForTest(Defaults())
	t.Cleanup(func() { resetForTest(Defaults()) })
	got := GetConfig()
	got.IPHeaders[0] = "MUTATED"
	if again := GetConfig(); again.IPHeaders[0] == "MUTATED" {
		t.Fatal("GetConfig aliases the active header order")
	}
}
