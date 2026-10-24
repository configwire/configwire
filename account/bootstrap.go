// First-superuser bootstrap from CONFIGWIRE_ADMIN_EMAIL/PASSWORD;
// fail-soft (skip paths log names only, never values, and boot continues).
package account

import (
	"log"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Values never logged.
const (
	envAdminEmail    = "CONFIGWIRE_ADMIN_EMAIL"
	envAdminPassword = "CONFIGWIRE_ADMIN_PASSWORD"
)

// Duplicate-email save maps to skip (already bootstrapped).
func isDuplicateSave(err error) bool {
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "unique") ||
		strings.Contains(lowered, "duplicate") ||
		strings.Contains(lowered, "already exists")
}

// BootstrapAdminFromEnv creates the first superuser when both vars are
// set and none exists yet. Both empty is a silent no-op (setup UI at /
// unchanged); every other skip is fail-soft. Idempotent. Email trimmed,
// password keeps inner spaces (trailing CR/LF stripped); only email logged.
func BootstrapAdminFromEnv(app core.App) error {
	email := strings.TrimSpace(os.Getenv(envAdminEmail))
	password := strings.TrimRight(os.Getenv(envAdminPassword), "\r\n")

	emailEmpty := email == ""
	passwordEmpty := strings.TrimSpace(password) == ""

	if emailEmpty && passwordEmpty {
		return nil
	}
	if emailEmpty != passwordEmpty {
		log.Printf("account: admin bootstrap skipped: set both %s and %s to auto-create the first superuser",
			envAdminEmail, envAdminPassword)
		return nil
	}
	if err := ValidateCredentials(email, password); err != nil {
		log.Printf("account: admin bootstrap skipped: invalid credentials: %v", err)
		return nil
	}
	n, err := countRealSuperusers(app)
	if err != nil {
		log.Printf("account: admin bootstrap skipped: failed to check superusers: %v", err)
		return nil
	}
	if !NeedsSetup(n) {
		log.Print("account: admin bootstrap skipped: superuser already exists")
		return nil
	}
	// Locked re-check plus write: concurrent boots serialize, the
	// loser observes the winner's row and skips.
	accountMu.Lock()
	defer accountMu.Unlock()
	if n, err := countRealSuperusers(app); err != nil {
		log.Printf("account: admin bootstrap skipped: failed to check superusers: %v", err)
		return nil
	} else if !NeedsSetup(n) {
		log.Print("account: admin bootstrap skipped: superuser already exists")
		return nil
	}
	if _, err := createSuperuser(app, email, password); err != nil {
		if isDuplicateSave(err) {
			log.Print("account: admin bootstrap skipped: superuser already exists")
			return nil
		}
		log.Printf("account: admin bootstrap skipped: failed to create superuser: %v", err)
		return nil
	}
	log.Printf("account: admin bootstrapped from env email=%s", email)
	return nil
}
