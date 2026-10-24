package account

import (
	"testing"
)

// setBootstrapEnv pins the admin env vars for one bootstrap case.
func setBootstrapEnv(t *testing.T, email, password string) {
	t.Helper()
	t.Setenv(envAdminEmail, "")
	t.Setenv(envAdminPassword, "")
	if email != "" {
		t.Setenv(envAdminEmail, email)
	}
	if password != "" {
		t.Setenv(envAdminPassword, password)
	}
}

func TestBootstrapAdminFromEnvEmptyNoop(t *testing.T) {
	app := accountTestApp(t)
	setBootstrapEnv(t, "", "")
	if err := BootstrapAdminFromEnv(app); err != nil {
		t.Fatalf("BootstrapAdminFromEnv: unexpected error %v", err)
	}
	if n, err := countRealSuperusers(app); err != nil || n != 0 {
		t.Fatalf("empty env: count=%d err=%v, want 0 (setup UI path)", n, err)
	}
}

func TestBootstrapAdminFromEnvOneSidedSkip(t *testing.T) {
	for _, c := range []struct {
		name     string
		email    string
		password string
	}{
		{"email only", "admin@example.com", ""},
		{"password only", "", "password123"},
	} {
		t.Run(c.name, func(t *testing.T) {
			app := accountTestApp(t)
			setBootstrapEnv(t, c.email, c.password)
			if err := BootstrapAdminFromEnv(app); err != nil {
				t.Fatalf("BootstrapAdminFromEnv: unexpected error %v", err)
			}
			if n, err := countRealSuperusers(app); err != nil || n != 0 {
				t.Fatalf("one-sided env: count=%d err=%v, want 0", n, err)
			}
		})
	}
}

func TestBootstrapAdminFromEnvInvalidNoCreate(t *testing.T) {
	app := accountTestApp(t)
	setBootstrapEnv(t, "not-an-email", "short")
	if err := BootstrapAdminFromEnv(app); err != nil {
		t.Fatalf("BootstrapAdminFromEnv: unexpected error %v", err)
	}
	if n, err := countRealSuperusers(app); err != nil || n != 0 {
		t.Fatalf("invalid creds: count=%d err=%v, want 0", n, err)
	}
}

func TestBootstrapAdminFromEnvCreatesOnceThenSkips(t *testing.T) {
	app := accountTestApp(t)
	setBootstrapEnv(t, "admin@example.com", "password123")
	if err := BootstrapAdminFromEnv(app); err != nil {
		t.Fatalf("first BootstrapAdminFromEnv: unexpected error %v", err)
	}
	if n, err := countRealSuperusers(app); err != nil || n != 1 {
		t.Fatalf("valid env: count=%d err=%v, want 1", n, err)
	}
	if err := BootstrapAdminFromEnv(app); err != nil {
		t.Fatalf("second BootstrapAdminFromEnv: unexpected error %v", err)
	}
	if n, err := countRealSuperusers(app); err != nil || n != 1 {
		t.Fatalf("second call: count=%d err=%v, want 1", n, err)
	}
}
