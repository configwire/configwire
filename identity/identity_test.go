package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/configwire/configwire/limits"
)

// TestDeriveAnonID_Vector pins the exact contract: hex(HMAC_SHA256(salt,
// ip + 0x00 + ua))[:16], 16 lowercase hex (isValidUserHash-compatible).
func TestDeriveAnonID_Vector(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	ip, ua := "203.0.113.7", "ConfigWire/1.0"

	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(ip))
	mac.Write([]byte{0x00})
	mac.Write([]byte(ua))
	want := hex.EncodeToString(mac.Sum(nil))[:16]

	got := DeriveAnonID(salt, ip, ua)
	if got != want {
		t.Fatalf("DeriveAnonID = %q, want %q", got, want)
	}
	if len(got) != 16 {
		t.Fatalf("anonID length = %d, want 16", len(got))
	}
	if !IsValidAnonID(got) {
		t.Fatalf("DeriveAnonID output %q fails IsValidAnonID (16 lowercase hex contract)", got)
	}
}

// TestDeriveAnonID_SeparatorPreventsAmbiguity proves the 0x00 separator:
// ("ab","c") and ("a","bc") must not collide the way naive concat would.
func TestDeriveAnonID_SeparatorPreventsAmbiguity(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	a := DeriveAnonID(salt, "ab", "c")
	b := DeriveAnonID(salt, "a", "bc")
	if a == b {
		t.Fatal("separator failure: DeriveAnonID(s,ab,c) == DeriveAnonID(s,a,bc)")
	}
}

// TestDeriveAnonID_Deterministic: same inputs always agree (sticky in-day).
func TestDeriveAnonID_Deterministic(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	if a, b := DeriveAnonID(salt, "1.2.3.4", "ua"), DeriveAnonID(salt, "1.2.3.4", "ua"); a != b {
		t.Fatalf("non-deterministic: %q != %q", a, b)
	}
}

// TestCountryDefaultsMatchLimits locks the two default orders together:
// the tunable lives in limits, but the identity backstop must agree or
// standalone CountryFromHeaders diverges from serve.
func TestCountryDefaultsMatchLimits(t *testing.T) {
	if got, want := strings.Join(countryHeaders, ","), strings.Join(limits.DefaultCountryHeaders(), ","); got != want {
		t.Fatalf("identity backstop = %q, limits defaults = %q, want equal", got, want)
	}
}

// TestCrossDayUnlinkability (Q1 daily rebucket, ACCEPTED):
// same env/ip/ua on different UTC days yields different anonIDs, and
// different envs on the same day also diverge.
func TestCrossDayUnlinkability(t *testing.T) {
	m := NewManager(NewMemoryStore())
	ip, ua := "203.0.113.7", "ConfigWire/1.0"
	day1 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	a, err := m.AnonID("env-a", ip, ua, day1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.AnonID("env-a", ip, ua, day2)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("linkable across days: same env/ip/ua derived the same anonID on two UTC days")
	}
	c, err := m.AnonID("env-b", ip, ua, day1)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("linkable across envs: two envIDs derived the same anonID for one day")
	}
	// Same env/day is sticky.
	a2, err := m.AnonID("env-a", ip, ua, day1)
	if err != nil {
		t.Fatal(err)
	}
	if a != a2 {
		t.Fatalf("not sticky in-day: %q != %q", a, a2)
	}
}

// TestNoRawIPPersisted is the behavioral half of the PII proof: after
// deriving IDs with marker IP/UA strings, no stored salt value or store
// key may contain those markers. Only (envID, day, salt) is persisted.
func TestNoRawIPPersisted(t *testing.T) {
	st := NewMemoryStore()
	m := NewManager(st)
	ip, ua := "marker-ip-198.51.100.99", "marker-ua-Test/9.9"
	if _, err := m.AnonID("env-a", ip, ua, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for k, v := range st.Dump() {
		if strings.Contains(k, ip) || strings.Contains(k, ua) {
			t.Fatalf("store key %q embeds raw IP/UA", k)
		}
		for _, b := range v {
			if strings.Contains(string(b), ip) || strings.Contains(string(b), ua) {
				t.Fatalf("stored salt for %q embeds raw IP/UA", k)
			}
		}
	}
}

// TestNoRawIPCapture is the static half of the PII proof: non-test
// sources in this package must never touch RemoteAddr / client-IP
// parsing — IP/UA arrive as opaque strings and are hashed, never read
// from the network here. (Trusted IP extraction stays in limits.ClientIP,
// the future caller.)
func TestNoRawIPCapture(t *testing.T) {
	srcs := testOnlyReadPackageSources(t, false)
	for name, src := range srcs {
		for _, banned := range []string{"RemoteAddr", "net.ParseIP", "ParseCIDR"} {
			if strings.Contains(src, banned) {
				t.Fatalf("%s contains %q: identity must not capture IPs, only hash them", name, banned)
			}
		}
	}
}

// TestStdlibOnlyGeoDeps pins Q2: no third-party GeoIP imports in this
// package's non-test sources, except pb_store.go — the durable salt
// wiring over the identity_salts collection necessarily imports
// PocketBase/dbx, while geo itself (geo.go) stays stdlib-only,
// header-read only. It parses real import paths (not raw substrings,
// so docs may name the rejected vendors) and requires every other
// import to be stdlib.
func TestStdlibOnlyGeoDeps(t *testing.T) {
	srcs := testOnlyReadPackageSources(t, false)
	for name, src := range srcs {
		if name == "pb_store.go" {
			continue
		}
		for _, imp := range testOnlyImportsOf(t, src) {
			if strings.Contains(imp, ".") {
				t.Fatalf("%s imports %q: identity must stay stdlib-only (no GeoIP deps)", name, imp)
			}
		}
	}
}
