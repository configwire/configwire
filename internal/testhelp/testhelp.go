// Package testhelp extracts the shared shapes duplicated across handler
// tests (account, stream, sso, releases): unwrapping router ApiErrors for
// non-2xx paths and seeding one _superusers row. Helpers take all state
// as params and keep none, so tests stay isolated.
package testhelp

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Code maps a direct handler invocation to its HTTP status: the
// router.ApiError status for non-2xx paths, -1 for a non-ApiError, or the
// recorder code for the 2xx path. Pure: never fails the test, so existing
// call* helpers keep their exact (int, string) return shapes.
func Code(err error, rec *httptest.ResponseRecorder) int {
	code, _ := CodeMessage(err, rec)
	return code
}

// CodeMessage maps a direct handler invocation to (status, message): the
// ApiError status + message for non-2xx paths, (-1, err text) for a
// non-ApiError, or (rec.Code, "") for the 2xx path.
func CodeMessage(err error, rec *httptest.ResponseRecorder) (int, string) {
	if err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status, apiErr.Message
		}
		return -1, err.Error()
	}
	return rec.Code, ""
}

// AssertCode fails the test unless the handler invocation produced want:
// ApiError status for non-2xx paths, rec.Code for the 2xx path. A
// non-ApiError always fails (it has no HTTP status).
func AssertCode(t *testing.T, rec *httptest.ResponseRecorder, err error, want int) {
	t.Helper()
	if err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			if apiErr.Status != want {
				t.Fatalf("handler error status = %d, want %d (message %q)", apiErr.Status, want, apiErr.Message)
			}
			return
		}
		t.Fatalf("handler returned non-ApiError %v, want status %d", err, want)
	}
	if rec.Code != want {
		t.Fatalf("recorder status = %d, want %d", rec.Code, want)
	}
}

// MustSuperuser seeds one _superusers row with the standard test password
// and returns it. It Fatals on setup failure; callers pass their own app
// so no state is shared between tests.
func MustSuperuser(t *testing.T, app core.App, email string) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("_superusers")
	if err != nil {
		t.Fatalf("find superusers collection: %v", err)
	}
	rec := core.NewRecord(collection)
	rec.SetEmail(strings.TrimSpace(email))
	rec.SetPassword("password123")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save superuser %s: %v", email, err)
	}
	return rec
}
