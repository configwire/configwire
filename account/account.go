// Package account wires first-run admin setup and superuser account
// management.
//
// First-run setup is PUBLIC by design (no superuser can exist yet to
// authenticate): GET /api/v1/admin/setup/status reports whether any
// _superusers row exists, and POST /api/v1/admin/setup creates the
// first superuser exactly once (409 once one exists). All other
// routes are superuser-only via apis.RequireSuperuserAuth().
// Token issuance stays out of scope: login happens via the existing
// auth-with-password collection API.
package account

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/configwire/configwire/security"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// superusersCollection is the PocketBase auth collection holding admins.
// The installer placeholder row (core.DefaultInstallerEmail,
// "__pbinstaller@example.com") is PocketBase's own first-run affordance:
// it exists on a fresh DB, carries an unknown random password, and is
// auto-deleted when the first real superuser is created. It must never
// count as a configured admin — this mirrors PocketBase's own
// needInstallerSuperuser check (count WHERE email != installer).
const superusersCollection = core.CollectionNameSuperusers

// Password bounds (PocketBase auth-field compatible: bcrypt caps at 72).
const (
	minPasswordLength = 8
	maxPasswordLength = 72
)

// maxEmailLength caps the trimmed email (matches the _superusers email field).
const maxEmailLength = 255

// Register mounts the public setup routes (no auth middleware: no
// superuser can exist yet on first run) and the superuser-only account
// routes (missing/invalid auth -> 401 via RequireSuperuserAuth).
func Register(se *core.ServeEvent) {
	se.Router.GET("/api/v1/admin/setup/status", getSetupStatus)
	se.Router.POST("/api/v1/admin/setup", postSetup)
	se.Router.GET("/api/v1/admin/account/list", listAccounts).Bind(apis.RequireSuperuserAuth())
	se.Router.POST("/api/v1/admin/account/create", createAccount).Bind(apis.RequireSuperuserAuth())
	se.Router.POST("/api/v1/admin/account/{id}/password", setPassword).Bind(apis.RequireSuperuserAuth())
	se.Router.DELETE("/api/v1/admin/account/{id}", deleteAccount).Bind(apis.RequireSuperuserAuth())
}

// NeedsSetup reports whether first-run setup is still pending: true
// when no superuser row exists. Pure (no I/O) so it is unit-testable.
func NeedsSetup(count int64) bool {
	return count == 0
}

// ValidateCredentials validates an email+password pair purely (no I/O),
// so it is unit-testable. Email first, then password.
func ValidateCredentials(email, password string) error {
	if err := validateEmail(email); err != nil {
		return err
	}
	return validatePassword(password)
}

// validateEmail enforces: trimmed non-empty, max 255 chars, exactly one
// @ with non-empty local/domain parts and a dot in the domain.
func validateEmail(email string) error {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return errInvalidEmail("email must not be empty.")
	}
	if len(trimmed) > maxEmailLength {
		return errInvalidEmail("email must be at most 255 characters.")
	}
	if strings.Count(trimmed, "@") != 1 {
		return errInvalidEmail("email must contain exactly one @.")
	}
	local, domain, _ := strings.Cut(trimmed, "@")
	if local == "" || domain == "" {
		return errInvalidEmail("email must have non-empty local and domain parts.")
	}
	if !strings.Contains(domain, ".") {
		return errInvalidEmail("email domain must contain a dot.")
	}
	if strings.EqualFold(trimmed, core.DefaultInstallerEmail) {
		return errInvalidEmail("email is reserved for the system installer.")
	}
	return nil
}

// validatePassword enforces length 8..72 chars (rune count; the 72 cap
// matches the bcrypt limit on the auth password field).
func validatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	if n < minPasswordLength || n > maxPasswordLength {
		return errInvalidPassword("password must be 8-72 characters.")
	}
	return nil
}

type credentialsError struct{ msg string }

func (e *credentialsError) Error() string { return e.msg }

func errInvalidEmail(msg string) error {
	return &credentialsError{msg: "invalid email: " + msg}
}

func errInvalidPassword(msg string) error {
	return &credentialsError{msg: "invalid password: " + msg}
}

// Empty bodies -> 400 (BindBody alone would silently return nil);
// malformed JSON or wrong field types -> 400. Body bytes are the
// emptiness source of truth (ContentLength is -1 for chunked).
func decodeBody(re *core.RequestEvent, dst any) error {
	if re.Request.ContentLength == 0 {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	if re.Request.Body == nil {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(re.Request.Body, 1<<20+1))
	if err != nil {
		return re.BadRequestError("malformed JSON body: "+err.Error(), nil)
	}
	// Restore for BindBody.
	re.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(bytes.TrimSpace(raw)) == 0 {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	if err := re.BindBody(dst); err != nil {
		return re.BadRequestError("malformed JSON body: "+err.Error(), nil)
	}
	return nil
}

// countRealSuperusers returns the number of configured admins, excluding
// PocketBase's installer placeholder row (unknown random password, not a
// loginable admin). A fresh DB yields 0. Only genuine DB failures surface.
func countRealSuperusers(app core.App) (int64, error) {
	recs, err := app.FindAllRecords(superusersCollection)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, r := range recs {
		if strings.EqualFold(r.GetString("email"), core.DefaultInstallerEmail) {
			continue
		}
		n++
	}
	return n, nil
}

// mapSaveError maps a superuser save failure to a request error without
// leaking internals: duplicate-email violations -> 409, anything else ->
// 400 carrying only the underlying message.
func mapSaveError(re *core.RequestEvent, err error) error {
	msg := err.Error()
	lowered := strings.ToLower(msg)
	if strings.Contains(lowered, "unique") ||
		strings.Contains(lowered, "duplicate") ||
		strings.Contains(lowered, "already exists") {
		return re.JSON(http.StatusConflict, map[string]any{
			"message": msg,
			"status":  http.StatusConflict,
		})
	}
	return re.BadRequestError(msg, nil)
}

// createSuperuser persists one _superusers row via the caller's
// request-scoped app (never a captured app). Validation is the caller's
// job; save failures (e.g. duplicate email) surface raw for mapping.
func createSuperuser(app core.App, email, password string) (*core.Record, error) {
	collection, err := app.FindCollectionByNameOrId(superusersCollection)
	if err != nil {
		return nil, err
	}
	rec := core.NewRecord(collection)
	rec.SetEmail(strings.TrimSpace(email))
	rec.SetPassword(password)
	if err := app.Save(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

type setupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// getSetupStatus handles GET /api/v1/admin/setup/status (PUBLIC).
// 200 {"needsSetup": bool} where needsSetup = (real superuser count == 0,
// installer placeholder excluded). Fresh DB answers needsSetup:true;
// DB failures answer 500.
func getSetupStatus(re *core.RequestEvent) error {
	security.SetHeaders(re)
	n, err := countRealSuperusers(re.App)
	if err != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "failed to check setup status: " + err.Error(),
			"status":  http.StatusInternalServerError,
		})
	}
	return re.JSON(http.StatusOK, map[string]any{"needsSetup": NeedsSetup(n)})
}

// postSetup handles POST /api/v1/admin/setup (PUBLIC).
// Order: 409 (a superuser already exists) -> 400 (validation) ->
// 201 {"id","email"}. Save-time duplicates (setup race) map via
// mapSaveError. No token is issued: login uses auth-with-password.
func postSetup(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req setupRequest
	if err := decodeBody(re, &req); err != nil {
		return err
	}
	n, err := countRealSuperusers(re.App)
	if err != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "failed to check setup status: " + err.Error(),
			"status":  http.StatusInternalServerError,
		})
	}
	if !NeedsSetup(n) {
		return re.JSON(http.StatusConflict, map[string]any{
			"message": "setup already completed: a superuser already exists.",
			"status":  http.StatusConflict,
		})
	}
	if err := ValidateCredentials(req.Email, req.Password); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	rec, err := createSuperuser(re.App, req.Email, req.Password)
	if err != nil {
		return mapSaveError(re, err)
	}
	email := strings.TrimSpace(req.Email)
	log.Printf("account: initial superuser created email=%s", email)
	return re.JSON(http.StatusCreated, map[string]any{"id": rec.Id, "email": email})
}

// listAccounts handles GET /api/v1/admin/account/list (superuser-only).
// 200 {"items":[{"id","email","created"}]}; the installer placeholder is
// never listed. Empty table -> {"items":[]}.
func listAccounts(re *core.RequestEvent) error {
	security.SetHeaders(re)
	recs, err := re.App.FindAllRecords(superusersCollection)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		if strings.EqualFold(r.GetString("email"), core.DefaultInstallerEmail) {
			continue
		}
		items = append(items, map[string]any{
			"id":      r.Id,
			"email":   r.GetString("email"),
			"created": r.GetDateTime("created").Time().UTC().Format(time.RFC3339),
		})
	}
	return re.JSON(http.StatusOK, map[string]any{"items": items})
}

// createAccount handles POST /api/v1/admin/account/create
// (superuser-only). Order: 400 (validation) -> 409 (duplicate email via
// save-error mapping) -> 201 {"id","email"}.
func createAccount(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req setupRequest
	if err := decodeBody(re, &req); err != nil {
		return err
	}
	if err := ValidateCredentials(req.Email, req.Password); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	rec, err := createSuperuser(re.App, req.Email, req.Password)
	if err != nil {
		return mapSaveError(re, err)
	}
	email := strings.TrimSpace(req.Email)
	log.Printf("account: superuser created email=%s", email)
	return re.JSON(http.StatusCreated, map[string]any{"id": rec.Id, "email": email})
}

type passwordRequest struct {
	Password string `json:"password"`
}

// setPassword handles POST /api/v1/admin/account/{id}/password
// (superuser-only). Order: 404 (unknown id) -> 400 (weak password) ->
// 200 {"id"}. The email is never changed here.
func setPassword(re *core.RequestEvent) error {
	security.SetHeaders(re)
	id := re.Request.PathValue("id")
	rec, err := re.App.FindRecordById(superusersCollection, id)
	if err != nil {
		return re.NotFoundError("unknown superuser id.", nil)
	}
	var req passwordRequest
	if err := decodeBody(re, &req); err != nil {
		return err
	}
	if err := validatePassword(req.Password); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	rec.SetPassword(req.Password)
	if err := re.App.Save(rec); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	log.Printf("account: superuser password updated id=%s", rec.Id)
	return re.JSON(http.StatusOK, map[string]any{"id": rec.Id})
}

// deleteAccount handles DELETE /api/v1/admin/account/{id}
// (superuser-only). Order: 404 (unknown id) -> 400 (would delete the
// last remaining superuser) -> 200 {"id"}.
func deleteAccount(re *core.RequestEvent) error {
	security.SetHeaders(re)
	id := re.Request.PathValue("id")
	rec, err := re.App.FindRecordById(superusersCollection, id)
	if err != nil {
		return re.NotFoundError("unknown superuser id.", nil)
	}
	if strings.EqualFold(rec.GetString("email"), core.DefaultInstallerEmail) {
		return re.BadRequestError("cannot delete the system installer account.", nil)
	}
	n, err := countRealSuperusers(re.App)
	if err != nil {
		return err
	}
	if n <= 1 {
		return re.BadRequestError("cannot delete the last remaining superuser.", nil)
	}
	if err := re.App.Delete(rec); err != nil {
		return err
	}
	log.Printf("account: superuser deleted id=%s", id)
	return re.JSON(http.StatusOK, map[string]any{"id": id})
}
