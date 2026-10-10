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
		{"inner space", "a b@c.de", "password123", false},
		{"tab in domain", "a@b\tc.de", "password123", false},
		{"control char", "a@b\x7fc.de", "password123", false},
		{"trailing newline trimmed", "admin@example.com\n", "password123", true},
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
// concurrency tests start from a genuinely fresh setup state. The
// last-admin delete guard is bound exactly as in production, so handler
// and data-API deletes run the same hooks as the live server.
func accountTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	RegisterGuard(app)
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

func TestDeleteAccountLastAdminBlocked(t *testing.T) {
	app := accountTestApp(t)
	rec, err := createSuperuser(app, "solo@example.com", "password123")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	code, msg := callDeleteAccount(app, rec.Id)
	if code != http.StatusBadRequest {
		t.Fatalf("delete last admin: got status %d, want 400", code)
	}
	if !strings.Contains(msg, "last remaining superuser") {
		t.Fatalf("delete last admin message = %q, want it to mention the last remaining superuser", msg)
	}
	if count, err := countRealSuperusers(app); err != nil || count != 1 {
		t.Fatalf("after blocked delete: count=%d err=%v, want the 1 superuser kept", count, err)
	}
}

func TestSuperuserDeleteHookCoversDataAPI(t *testing.T) {
	app := accountTestApp(t)
	only, err := createSuperuser(app, "hook-solo@example.com", "password123")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := app.Delete(only); err == nil {
		t.Fatalf("direct delete of the last admin succeeded, want refusal")
	} else if lowered := strings.ToLower(err.Error()); !strings.Contains(lowered, "superuser") {
		t.Fatalf("direct delete error = %q, want it to mention the superuser guard", err)
	}
	second, err := createSuperuser(app, "hook-second@example.com", "password123")
	if err != nil {
		t.Fatalf("create second admin: %v", err)
	}
	if err := app.Delete(second); err != nil {
		t.Fatalf("direct delete with 2 admins: unexpected error %v", err)
	}
	if count, err := countRealSuperusers(app); err != nil || count != 1 {
		t.Fatalf("after data-API delete: count=%d err=%v, want exactly 1 superuser left", count, err)
	}
}

// TestInternalErrorHidesDetail pins the error-hygiene contract: DB text
// goes to the server log only; callers get a generic 500 body.
func TestInternalErrorHidesDetail(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account/list", nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{}
	re.Request = req
	re.Response = rec
	// Like the inline 500 blocks (re.JSON returns nil after writing),
	// internalError reports via the recorder, not a non-nil error.
	if err := internalError(re, "failed to list superusers", errors.New("driver exploded: connection reset")); err != nil {
		t.Fatalf("internalError returned %v, want nil after writing", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("recorder status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "internal error") || strings.Contains(body, "exploded") {
		t.Fatalf("500 body = %q, want generic text without driver detail", body)
	}
}

// TestListAccountsDBFailure500 pins that a storage failure on the list
// path reads as an outage (generic 500), never raw DB text.
func TestListAccountsDBFailure500(t *testing.T) {
	app := accountTestApp(t)
	// System collections refuse app.Delete; drop the table directly to
	// force the storage failure (test-only fixture sabotage).
	if _, err := app.DB().NewQuery("DROP TABLE " + superusersCollection).Execute(); err != nil {
		t.Fatalf("drop superusers table: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account/list", nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	// internalError reports via the recorder (re.JSON returns nil after
	// writing), so a nil error with a generic 500 body is the pass shape.
	if err := listAccounts(re); err != nil {
		t.Fatalf("list failure returned %v, want nil after writing", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list failure status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "internal error") || strings.Contains(body, "no such table") {
		t.Fatalf("500 body = %q, want generic text without driver detail", body)
	}
}

// callSetPassword invokes the setPassword handler directly, reporting
// the HTTP status plus the ApiError message for non-2xx paths.
func callSetPassword(app core.App, id, password string) (int, string) {
	body := strings.NewReader(`{"password":"` + password + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account/"+id+"/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := setPassword(re); err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status, apiErr.Message
		}
		return -1, err.Error()
	}
	return rec.Code, ""
}

// TestSetPasswordSaveFailureGeneric pins that a storage failure on save
// answers a generic 400 (mapSaveError phrasing), never raw DB text.
func TestSetPasswordSaveFailureGeneric(t *testing.T) {
	app := accountTestApp(t)
	rec, err := createSuperuser(app, "pw-target@example.com", "password123")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	app.OnRecordUpdate(superusersCollection).BindFunc(func(e *core.RecordEvent) error {
		return errors.New("db exploded: connection reset")
	})
	code, msg := callSetPassword(app, rec.Id, "newpassword123")
	if code != http.StatusBadRequest {
		t.Fatalf("save failure status = %d, want 400", code)
	}
	// router.ApiError sentenizes ("Could not update password.").
	if msg != "Could not update password." {
		t.Fatalf("save failure message = %q, want the generic body", msg)
	}
	if strings.Contains(msg, "exploded") {
		t.Fatalf("400 body = %q, must not leak driver text", msg)
	}
}

// TestDeleteAccountWriteFailureGeneric pins that a storage failure on
// delete answers a generic 400, never raw DB text.
func TestDeleteAccountWriteFailureGeneric(t *testing.T) {
	app := accountTestApp(t)
	if _, err := createSuperuser(app, "doomed-a@example.com", "password123"); err != nil {
		t.Fatalf("create admin a: %v", err)
	}
	victim, err := createSuperuser(app, "doomed-b@example.com", "password123")
	if err != nil {
		t.Fatalf("create admin b: %v", err)
	}
	app.OnRecordDelete(superusersCollection).BindFunc(func(e *core.RecordEvent) error {
		return errors.New("db exploded: connection reset")
	})
	code, msg := callDeleteAccount(app, victim.Id)
	if code != http.StatusBadRequest {
		t.Fatalf("delete failure status = %d, want 400", code)
	}
	// router.ApiError sentenizes ("Could not delete account.").
	if msg != "Could not delete account." {
		t.Fatalf("delete failure message = %q, want the generic body", msg)
	}
	if strings.Contains(msg, "exploded") {
		t.Fatalf("400 body = %q, must not leak driver text", msg)
	}
}
