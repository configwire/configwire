package account

import (
	"strings"
	"testing"
)

func TestValidateCredentials(t *testing.T) {
	cases := []struct {
		name     string
		email    string
		password string
		ok       bool
	}{
		{"valid", "admin@example.com", "password123", true},                        // happy path
		{"valid min password", "a@b.cd", "12345678", true},                         // 8-char floor
		{"valid max password", "admin@example.com", strings.Repeat("x", 72), true}, // 72-char ceiling
		{"valid surrounding spaces", "  admin@example.com  ", "password123", true}, // trimmed
		{"empty email", "", "password123", false},
		{"blank email", "   ", "password123", false},
		{"missing at", "adminexample.com", "password123", false},
		{"two ats", "a@b@c.com", "password123", false},
		{"empty local", "@example.com", "password123", false},
		{"empty domain", "admin@", "password123", false},
		{"domain without dot", "admin@localhost", "password123", false},
		{"installer email reserved", "__pbinstaller@example.com", "password123", false},
		{"email too long", strings.Repeat("a", 250) + "@b.cd.us", "password123", false}, // 257 > 255
		{"short password", "admin@example.com", "1234567", false},                       // 7 chars
		{"empty password", "admin@example.com", "", false},
		{"long password", "admin@example.com", strings.Repeat("x", 73), false}, // 73 chars
		{"both invalid", "nope", "short", false},                               // email reported first
	}
	for _, c := range cases {
		err := ValidateCredentials(c.email, c.password)
		if c.ok && err != nil {
			t.Errorf("%s: ValidateCredentials(%q, ...): unexpected error %v", c.name, c.email, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: ValidateCredentials(%q, ...): expected error, got nil", c.name, c.email)
		}
	}
}

func TestNeedsSetup(t *testing.T) {
	cases := []struct {
		count int64
		want  bool
	}{
		{0, true},  // empty DB: first run
		{1, false}, // one superuser: setup done
		{5, false}, // several superusers: setup done
	}
	for _, c := range cases {
		if got := NeedsSetup(c.count); got != c.want {
			t.Errorf("NeedsSetup(%d) = %v, want %v", c.count, got, c.want)
		}
	}
}
