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
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/configwire/configwire/security"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// accountMu serializes the check-then-write sequences in postSetup
// (count -> createSuperuser), deleteAccount (count fast-path), and the
// RegisterGuard delete hook (authoritative count inside the delete).
// Without it two concurrent requests can both observe count 0 (or n==2)
// and both write, creating a second superuser (or deleting the last
// admin). The lock is never held across decodeBody/validation, and the
// handler never holds it across Delete (the hook takes it): holding one
// non-reentrant mutex across a hooked Delete would deadlock.
var accountMu sync.Mutex

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

// lastSuperuserMsg is the stable 400 body for last-admin refusals, shared
// by the handler fast-path, the RegisterGuard hook, and the PocketBase
// core message normalization below.
const lastSuperuserMsg = "cannot delete the last remaining superuser."

// RegisterGuard blocks deletion of the last remaining admin on EVERY
// _superusers delete path: the custom DELETE endpoint, the data API
// (DELETE /api/collections/_superusers/records/:id), the dashboard, and
// server-side saves. The installer placeholder stays deletable so
// PocketBase can auto-remove it when the first real superuser is
// created. This hook is the authoritative check inside the delete; the
// handler keeps a fast-path check only for its 404 -> 400 -> 200 order.
func RegisterGuard(app core.App) {
	app.OnRecordDelete(superusersCollection).BindFunc(func(e *core.RecordEvent) error {
		if strings.EqualFold(e.Record.GetString("email"), core.DefaultInstallerEmail) {
			return e.Next()
		}
		accountMu.Lock()
		defer accountMu.Unlock()
		n, err := countRealSuperusers(e.App)
		if err != nil {
			return err
		}
		if n <= 1 {
			return apis.NewBadRequestError(lastSuperuserMsg, nil)
		}
		return e.Next()
	})
}

// isLastSuperuserErr reports whether err is a last-admin refusal from any
// layer: our handler/hook message or PocketBase core's own "only existing
// superuser" guard.
func isLastSuperuserErr(err error) bool {
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "last remaining superuser") ||
		strings.Contains(lowered, "only existing superuser")
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
	if strings.IndexFunc(trimmed, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errInvalidEmail("email must not contain spaces or control characters.")
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

// mapSaveError maps a superuser save failure to a request error with a
// generic body (never the raw DB/driver message): duplicate-email
// violations -> 409, anything else -> 400. The underlying message goes to
// the server log only.
func mapSaveError(re *core.RequestEvent, err error) error {
	msg := err.Error()
	log.Printf("account: failed to create superuser: %s", msg)
	lowered := strings.ToLower(msg)
	if strings.Contains(lowered, "unique") ||
		strings.Contains(lowered, "duplicate") ||
		strings.Contains(lowered, "already exists") {
		return re.JSON(http.StatusConflict, map[string]any{
			"message": "email already in use",
			"status":  http.StatusConflict,
		})
	}
	return re.BadRequestError("could not create account", nil)
}

// internalError logs err server-side and renders a generic 500: DB/driver
// text never reaches the caller. Read-side failures use this; write-side
// failures use the mapSaveError phrasing ("could not ...") via
// BadRequestError at each site.
func internalError(re *core.RequestEvent, msg string, err error) error {
	log.Printf("account: %s: %v", msg, err)
	return re.JSON(http.StatusInternalServerError, map[string]any{
		"message": "internal error",
		"status":  http.StatusInternalServerError,
	})
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
		log.Printf("account: failed to check setup status: %v", err)
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "internal error",
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
		log.Printf("account: failed to check setup status: %v", err)
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "internal error",
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
	// Locked re-check plus write: the pre-lock count/validation above is
	// only a fast path preserving the 409 -> 400 -> 201 order. The count
	// is repeated under accountMu so concurrent setups serialize: the
	// loser observes the winner's row and answers 409 (or maps the
	// save-time duplicate via mapSaveError).
	accountMu.Lock()
	defer accountMu.Unlock()
	if n, err := countRealSuperusers(re.App); err != nil {
		log.Printf("account: failed to check setup status: %v", err)
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "internal error",
			"status":  http.StatusInternalServerError,
		})
	} else if !NeedsSetup(n) {
		return re.JSON(http.StatusConflict, map[string]any{
			"message": "setup already completed: a superuser already exists.",
			"status":  http.StatusConflict,
		})
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
		return internalError(re, "failed to list superusers", err)
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
		log.Printf("account: failed to update superuser password id=%s: %v", rec.Id, err)
		return re.BadRequestError("could not update password", nil)
	}
	log.Printf("account: superuser password updated id=%s", rec.Id)
	return re.JSON(http.StatusOK, map[string]any{"id": rec.Id})
}

// deleteAccount handles DELETE /api/v1/admin/account/{id}
// (superuser-only). Order: 404 (unknown id) -> 400 (would delete the
// last remaining superuser) -> 200 {"id"}. The fast-path count below
// preserves the order; the RegisterGuard hook re-checks authoritatively
// inside Delete, so losers of concurrent deletes across any path still
// answer 400 and every refusal carries the same message.
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
	// Fast path only: the lock is released before Delete because the
	// hook takes the same mutex — holding it across a hooked Delete
	// would deadlock.
	accountMu.Lock()
	n, err := countRealSuperusers(re.App)
	accountMu.Unlock()
	if err != nil {
		return internalError(re, "failed to count superusers", err)
	}
	if n <= 1 {
		return re.BadRequestError(lastSuperuserMsg, nil)
	}
	if err := re.App.Delete(rec); err != nil {
		if isLastSuperuserErr(err) {
			return re.BadRequestError(lastSuperuserMsg, nil)
		}
		log.Printf("account: failed to delete superuser id=%s: %v", id, err)
		return re.BadRequestError("could not delete account", nil)
	}
	log.Printf("account: superuser deleted id=%s", id)
	return re.JSON(http.StatusOK, map[string]any{"id": id})
}
