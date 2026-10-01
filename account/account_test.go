package account

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
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

// accountTestApp boots a TestApp with every _superusers row removed, so
// concurrency tests start from a genuinely fresh setup state.
func accountTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	recs, err := app.FindAllRecords(superusersCollection)
	if err != nil {
		t.Fatalf("list superusers: %v", err)
	}
	// PocketBase validators refuse to delete the only superuser, so clear
	// the table hook-free: this is test fixture setup, not the path under
	// test (the handlers keep using the hooked app).
	unsafe := app.UnsafeWithoutHooks()
	for _, r := range recs {
		if err := unsafe.Delete(r); err != nil {
			t.Fatalf("delete superuser %s: %v", r.Id, err)
		}
	}
	if n, err := countRealSuperusers(app); err != nil || n != 0 {
		t.Fatalf("fresh app: count=%d err=%v, want 0", n, err)
	}
	return app
}

// callPostSetup invokes the postSetup handler directly and reports the
// HTTP status, unwrapping router ApiErrors for non-2xx paths.
func callPostSetup(app core.App, email string) int {
	body := strings.NewReader(`{"email":"` + email + `","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/setup", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postSetup(re); err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status
		}
		return -1
	}
	return rec.Code
}

// callDeleteAccount invokes the deleteAccount handler directly, reporting
// the HTTP status plus the ApiError message for non-2xx paths.
func callDeleteAccount(app core.App, id string) (int, string) {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/account/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := deleteAccount(re); err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status, apiErr.Message
		}
		return -1, err.Error()
	}
	return rec.Code, ""
}

func TestPostSetupConcurrentSingleWinner(t *testing.T) {
	app := accountTestApp(t)
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct emails: without the mutex every goroutine would pass
			// the count==0 check and create its own row, so only the
			// serialized count-then-create yields exactly one winner.
			codes[i] = callPostSetup(app, fmt.Sprintf("race-setup-%d@example.com", i))
		}(i)
	}
	wg.Wait()
	var created, conflicted, other int
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		default:
			other++
		}
	}
	if created != 1 || conflicted != n-1 || other != 0 {
		t.Fatalf("postSetup x%d: got %d created, %d conflicted, %d other; want 1, %d, 0",
			n, created, conflicted, other, n-1)
	}
	if count, err := countRealSuperusers(app); err != nil || count != 1 {
		t.Fatalf("after concurrent setup: count=%d err=%v, want exactly 1 superuser", count, err)
	}
}

func TestDeleteAccountConcurrentLastAdminKept(t *testing.T) {
	app := accountTestApp(t)
	recA, err := createSuperuser(app, "keep-a@example.com", "password123")
	if err != nil {
		t.Fatalf("create admin A: %v", err)
	}
	recB, err := createSuperuser(app, "keep-b@example.com", "password123")
	if err != nil {
		t.Fatalf("create admin B: %v", err)
	}
	type outcome struct {
		code int
		msg  string
	}
	out := make([]outcome, 2)
	var wg sync.WaitGroup
	for i, id := range []string{recA.Id, recB.Id} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			c, m := callDeleteAccount(app, id)
			out[i] = outcome{c, m}
		}(i, id)
	}
	wg.Wait()
	var deleted, refused int
	for _, o := range out {
		switch o.code {
		case http.StatusOK:
			deleted++
		case http.StatusBadRequest:
			refused++
			if !strings.Contains(o.msg, "last remaining superuser") {
				t.Errorf("delete refusal message = %q, want it to mention the last remaining superuser", o.msg)
			}
		default:
			t.Errorf("deleteAccount concurrent: unexpected status %d (%q)", o.code, o.msg)
		}
	}
	if deleted != 1 || refused != 1 {
		t.Fatalf("concurrent delete of 2 admins: got %d x200 and %d x400, want exactly 1 and 1",
			deleted, refused)
	}
	if count, err := countRealSuperusers(app); err != nil || count != 1 {
		t.Fatalf("after concurrent delete: count=%d err=%v, want exactly 1 superuser left", count, err)
	}
}
