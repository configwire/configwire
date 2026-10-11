// Package backups resolves auto-backup configuration from the environment
// without booting a server.
//
// It is intentionally pure (stdlib only: os/strconv/strings): it never
// imports PocketBase packages and never touches app.Settings(). The caller
// (startup wiring) applies the returned values and persists them.
package backups

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Environment variables handled by Resolve, following the CONFIGWIRE_*
// precedent in configwire/limits (invalid values fall back to leaving the
// stored value untouched).
const (
	// CronEnvVar holds a cron expression; empty/unset leaves stored cron untouched.
	CronEnvVar = "CONFIGWIRE_BACKUP_CRON"
	// KeepEnvVar holds the rotation count; unset/non-numeric/<=0 leaves stored keep untouched.
	KeepEnvVar = "CONFIGWIRE_BACKUP_KEEP"

	// S3EnabledEnvVar enables S3 backup storage; truthy 1/true/yes
	// enables, falsy 0/false/no disables, anything else (including
	// unset/empty/unparsable) leaves the stored enabled flag untouched.
	// Disabling via env IS supported: PocketBase S3Config.Validate only
	// requires bucket/region/endpoint/accessKey/secret when Enabled is
	// true (validation.When(c.Enabled, ...) in core/settings_model.go),
	// so Enabled=false always saves as a valid no-op. Clearing the
	// remaining fields still goes through PATCH /api/settings.
	S3EnabledEnvVar = "CONFIGWIRE_BACKUP_S3_ENABLED"
	// S3EndpointEnvVar holds the S3 endpoint URL; empty/unset leaves stored endpoint untouched.
	S3EndpointEnvVar = "CONFIGWIRE_BACKUP_S3_ENDPOINT"
	// S3BucketEnvVar holds the S3 bucket; empty/unset leaves stored bucket untouched.
	S3BucketEnvVar = "CONFIGWIRE_BACKUP_S3_BUCKET"
	// S3RegionEnvVar holds the S3 region; empty/unset leaves stored region untouched.
	S3RegionEnvVar = "CONFIGWIRE_BACKUP_S3_REGION"
	// S3AccessKeyEnvVar holds the S3 access key; empty/unset leaves stored key untouched.
	S3AccessKeyEnvVar = "CONFIGWIRE_BACKUP_S3_ACCESS_KEY"
	// S3SecretEnvVar holds the S3 secret; empty/unset leaves stored secret untouched.
	S3SecretEnvVar = "CONFIGWIRE_BACKUP_S3_SECRET"
	// S3ForcePathStyleEnvVar toggles S3 path-style addressing; truthy
	// 1/true/yes sets true, falsy 0/false/no sets false, anything else
	// (including unset/empty) leaves the stored flag untouched.
	S3ForcePathStyleEnvVar = "CONFIGWIRE_BACKUP_S3_FORCE_PATH_STYLE"
)

// DefaultCronMaxKeep is the rotation count applied when cron is newly set
// (stored cron was "") and the stored keep is 0/invalid (< 1).
//
// Choice rationale: PocketBase BackupsConfig.Validate requires CronMaxKeep
// >= 1 whenever Cron != "" (see core/settings_model.go: BackupsConfig.Validate
// with validation.When(c.Cron != "", validation.Required) plus
// validation.Min(1)). Emitting a non-empty cron with keep < 1 would therefore
// always fail validation at save time, so the resolver fills a documented
// default (10) instead of signalling deferral — keeping the caller fail-soft
// path simple. 10 matches a conventional small rotation window.
const DefaultCronMaxKeep = 10

// Resolve merges CONFIGWIRE_BACKUP_CRON / CONFIGWIRE_BACKUP_KEEP over the
// stored settings values and reports the values the caller should apply.
//
// currentCron and currentKeep are the stored settings values
// (app.Settings().Backups.Cron / CronMaxKeep). It returns the new values,
// whether they differ from the stored ones (changed=false means the caller
// must not rewrite), and a log message (always prefixed
// "configwire: backups ", never containing secrets — there are none here).
//
// Cron handling: an empty/unset CONFIGWIRE_BACKUP_CRON leaves the stored
// cron untouched. A non-empty value is returned as-is for the caller to
// apply; PocketBase validates it via cron.NewSchedule at settings-save time
// (see checkCronExpression in core/settings_model.go), and save errors are
// the caller's (startup wiring's) fail-soft responsibility — this function
// performs no cron syntax validation itself.
//
// Keep handling: an unset/empty/non-numeric/<=0 CONFIGWIRE_BACKUP_KEEP
// leaves the stored keep untouched (same invalid→default idiom as
// parseEnvInt in configwire/limits).
func Resolve(currentCron string, currentKeep int) (newCron string, newKeep int, changed bool, logMsg string) {
	newCron = currentCron
	newKeep = currentKeep

	if v := strings.TrimSpace(os.Getenv(CronEnvVar)); v != "" {
		newCron = v
	}

	if v := strings.TrimSpace(os.Getenv(KeepEnvVar)); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			newKeep = n
		}
	}

	if strings.TrimSpace(currentCron) == "" && newCron != "" && newKeep < 1 {
		newKeep = DefaultCronMaxKeep
	}

	changed = newCron != currentCron || newKeep != currentKeep
	if changed {
		logMsg = fmt.Sprintf("configwire: backups updated (cron=%q keep=%d)", newCron, newKeep)
	} else {
		logMsg = fmt.Sprintf("configwire: backups unchanged (cron=%q keep=%d)", newCron, newKeep)
	}
	return newCron, newKeep, changed, logMsg
}

// S3 mirrors the PocketBase S3Config fields (core/settings_model.go:
// Enabled, Bucket, Region, Endpoint, AccessKey, Secret, ForcePathStyle)
// in stdlib-only form so this package stays import-free of PocketBase.
// The caller maps it to/from app.Settings().Backups.S3.
type S3 struct {
	Enabled        bool
	Bucket         string
	Region         string
	Endpoint       string
	AccessKey      string
	Secret         string
	ForcePathStyle bool
}

// parseEnvBool maps a trimmed env value to (v, ok): 1/true/yes → true,
// 0/false/no → false (case-insensitive); anything else is unparsable.
func parseEnvBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "1", "true", "yes":
		return true, true
	case "0", "false", "no":
		return false, true
	default:
		return false, false
	}
}

// ResolveS3 merges the CONFIGWIRE_BACKUP_S3_* env vars over the stored S3
// values and reports the values the caller should apply.
//
// Per-field rule (same as cron/keep): only non-empty env values are
// applied; empty/unset values leave that stored field untouched, and an
// unparsable bool env is ignored fail-soft. Unset everything and the
// stored S3 config is returned untouched (changed=false, so the caller
// must not rewrite).
//
// It performs no S3 validation itself (PocketBase S3Config.Validate
// requires endpoint/bucket/region/accessKey/secret when enabled, checked
// at settings-save time); the log message names only the changed fields
// ("configwire: backups s3 updated (fields=...)") and never carries
// values, so secrets cannot leak into logs.
func ResolveS3(current S3) (newS3 S3, changed bool, logMsg string) {
	newS3 = current
	var fields []string

	if v := strings.TrimSpace(os.Getenv(S3EndpointEnvVar)); v != "" {
		newS3.Endpoint = v
		fields = append(fields, "endpoint")
	}
	if v := strings.TrimSpace(os.Getenv(S3BucketEnvVar)); v != "" {
		newS3.Bucket = v
		fields = append(fields, "bucket")
	}
	if v := strings.TrimSpace(os.Getenv(S3RegionEnvVar)); v != "" {
		newS3.Region = v
		fields = append(fields, "region")
	}
	if v := strings.TrimSpace(os.Getenv(S3AccessKeyEnvVar)); v != "" {
		newS3.AccessKey = v
		fields = append(fields, "access-key")
	}
	if v := strings.TrimSpace(os.Getenv(S3SecretEnvVar)); v != "" {
		newS3.Secret = v
		fields = append(fields, "secret")
	}
	if v := strings.TrimSpace(os.Getenv(S3ForcePathStyleEnvVar)); v != "" {
		if b, ok := parseEnvBool(v); ok {
			newS3.ForcePathStyle = b
			fields = append(fields, "force-path-style")
		}
	}
	if v := strings.TrimSpace(os.Getenv(S3EnabledEnvVar)); v != "" {
		if b, ok := parseEnvBool(v); ok {
			newS3.Enabled = b
			fields = append(fields, "enabled")
		}
	}

	changed = newS3 != current
	if changed {
		logMsg = fmt.Sprintf("configwire: backups s3 updated (fields=%s)", strings.Join(fields, ","))
	} else {
		logMsg = "configwire: backups s3 unchanged"
	}
	return newS3, changed, logMsg
}
