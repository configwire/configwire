// Package logstore owns the persisted operator log store, mirroring
// configwire/retention (log_settings singleton pattern) without any
// API/UI yet (a later step wires the viewer/settings endpoints).
//
// Model: server_logs rows carry level/message/src only — callers pass
// reason codes (missing-key, rate-limited, ip-blocked, admin-denied,
// storage-error). Never pass key material, tokens, emails, IPs, buckets,
// or attributes. Levels follow configwire/logx (debug 0 .. error 3).
// Rows older than MaxDays die via Purge. Tunables live in Settings,
// defaulted from code ({enabled, 30} with 30d of history), overridden by
// env, overridden at runtime by the log_settings collection row
// (key=global) via a future admin API.
//
// Why a singleton row: one operator-chosen policy per binary, same as
// retention.retention_settings — find-or-create on key="global", backed
// by a UNIQUE index on key.
//
// PB internal logs are untouched (Settings().Logs stays pinned in
// main.go); superuser-only enforcement lives in the future API layer,
// not here. All writes take a request-scoped app, never captured
// globals. Write returns errors to the caller — it never panics and
// never blocks beyond the single Save call.
package logstore

import (
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"
)

// Defaults mirror the intended operator history window: store on, 30d.
const (
	DefaultEnabled = true
	DefaultMaxDays = 30

	// MinDays / MaxDays bound the knob (1d..1825d = 5yr max, same as
	// retention).
	MinDays = 1
	MaxDays = 1825

	// DefaultSrc fills empty src on Write (PocketBase TextField has no
	// column default beyond ''), matching the logx greppable prefix.
	DefaultSrc = "configwire"

	// minLevel / maxLevel mirror logx.LevelDebug..LevelError.
	minLevel = 0
	maxLevel = 3
)

// Settings carries the tunable log-store policy.
type Settings struct {
	Enabled bool `json:"enabled"`
	MaxDays int  `json:"maxDays"`
}

// Defaults returns the code defaults (before env/DB overrides).
func Defaults() Settings {
	return Settings{
		Enabled: DefaultEnabled,
		MaxDays: DefaultMaxDays,
	}
}

// parseEnvInt reads an int env var, falling back to def on empty/invalid.
func parseEnvInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// parseEnvBool reads a bool env var, falling back to def on empty/invalid.
func parseEnvBool(name string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// loadDefaultsFromEnv applies the CONFIGWIRE_* overrides over Defaults.
// Invalid (non-numeric) values fall back to the default; out-of-range env
// values are clamped by clampSettings at package init and again in
// EnsureLoaded on the serve path.
func loadDefaultsFromEnv() Settings {
	c := Defaults()
	c.Enabled = parseEnvBool("CONFIGWIRE_LOG_STORE_ENABLED", c.Enabled)
	c.MaxDays = parseEnvInt("CONFIGWIRE_LOG_STORE_MAX_DAYS", c.MaxDays)
	return c
}

// ValidateSettings enforces 1..1825 on maxDays (enabled is a plain bool,
// always valid). Violations are 400-style settingsErrors for the future
// admin PUT path.
func ValidateSettings(c Settings) error {
	if c.MaxDays < MinDays || c.MaxDays > MaxDays {
		return &settingsError{msg: "invalid maxDays: must be 1..1825."}
	}
	return nil
}

type settingsError struct{ msg string }

func (e *settingsError) Error() string { return e.msg }

// clampSettings forces maxDays into 1..1825 (used when loading env/DB
// values that bypass admin validation), falling back to the code default.
func clampSettings(c Settings) Settings {
	d := Defaults()
	if c.MaxDays < MinDays || c.MaxDays > MaxDays {
		c.MaxDays = d.MaxDays
	}
	return c
}

// Module state: atomic settings under RWMutex (mirrors retention.current).
var (
	cfgMu   sync.RWMutex
	current = clampSettings(loadDefaultsFromEnv())
)

// resetForTest replaces settings (tests only, same package).
func resetForTest(c Settings) {
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
}

// GetConfig returns a snapshot of the active settings.
func GetConfig() Settings {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return current
}

// applyConfig validates and installs c (future admin PUT path).
func applyConfig(c Settings) error {
	if err := ValidateSettings(c); err != nil {
		return err
	}
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
	return nil
}

// UpdateSettings validates c, upserts the log_settings singleton row, and
// installs c in memory (future admin PUT path). Uses the request-scoped
// app (never captured).
func UpdateSettings(app core.App, c Settings) error {
	if err := ValidateSettings(c); err != nil {
		return err
	}
	if err := upsertGlobalRow(app, c); err != nil {
		return err
	}
	return applyConfig(c)
}

// collectionName is the PocketBase collection holding the singleton row.
const collectionName = "log_settings"

// logsCollection is the PocketBase collection holding persisted log rows.
const logsCollection = "server_logs"

// globalKey identifies the singleton settings row.
const globalKey = "global"

// EnsureLoaded seeds the singleton log_settings row when absent and loads
// DB values over env defaults. Missing collection (migration not yet
// applied) keeps env defaults with a warning — never fatal on serve.
// Exported so the future API layer (or serve wiring) can call it; the
// enforcement of superuser-only access lives in that layer, not here.
func EnsureLoaded(app core.App) {
	c := loadDefaultsFromEnv()
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		cfgMu.Lock()
		current = clampSettings(c)
		cfgMu.Unlock()
		log.Print("logstore: log_settings collection missing, using env defaults")
		return
	}
	rec, err := findGlobalRow(app)
	if err != nil {
		log.Printf("logstore: failed to read log_settings: %v, using env defaults", err)
		cfgMu.Lock()
		current = clampSettings(c)
		cfgMu.Unlock()
		return
	}
	if rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("key", globalKey)
		rec.Set("enabled", c.Enabled)
		rec.Set("maxDays", c.MaxDays)
		if err := app.Save(rec); err != nil {
			log.Printf("logstore: failed to seed log_settings: %v", err)
		}
		cfgMu.Lock()
		current = clampSettings(c)
		cfgMu.Unlock()
		return
	}
	c.Enabled = rec.GetBool("enabled")
	if v := rec.GetInt("maxDays"); v > 0 {
		c.MaxDays = v
	}
	cfgMu.Lock()
	current = clampSettings(c)
	cfgMu.Unlock()
}

// findGlobalRow returns the singleton row (key=global) or nil when absent.
func findGlobalRow(app core.App) (*core.Record, error) {
	recs, err := app.FindRecordsByFilter(
		collectionName,
		"key = {:key}",
		"",
		1, 0,
		dbx.Params{"key": globalKey},
	)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	return recs[0], nil
}

// upsertGlobalRow writes the singleton log_settings row (key=global),
// creating it when absent. Uses the request-scoped app (never captured).
func upsertGlobalRow(app core.App, c Settings) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}
	rec, err := findGlobalRow(app)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("key", globalKey)
	}
	rec.Set("enabled", c.Enabled)
	rec.Set("maxDays", c.MaxDays)
	return app.Save(rec)
}

// Write persists one reason-code line to server_logs. It no-ops (nil)
// when the store is disabled, returns an error on empty message or
// out-of-range level, and otherwise performs a single Save — callers must
// treat the error as best-effort (log to stderr, never retry-block the
// request). src defaults to "configwire" when blank. level/message/src
// only: callers must pass reason codes, never PII.
func Write(app core.App, level int, src, message string) error {
	if !GetConfig().Enabled {
		return nil
	}
	if level < minLevel || level > maxLevel {
		return &settingsError{msg: "invalid level: must be 0..3."}
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("logstore: message is required")
	}
	if strings.TrimSpace(src) == "" {
		src = DefaultSrc
	}
	collection, err := app.FindCollectionByNameOrId(logsCollection)
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	rec.Set("level", level)
	rec.Set("src", src)
	rec.Set("message", message)
	return app.Save(rec)
}

// cutoffFor maps now to the purge horizon (now minus maxDays).
func cutoffFor(now time.Time, maxDays int) time.Time {
	return now.UTC().AddDate(0, 0, -maxDays).Truncate(time.Millisecond)
}

// purgePageSize bounds one delete page: one page = one short transaction.
const purgePageSize = 500

// Purge deletes server_logs rows with created strictly older than
// now-MaxDays, paged by (created, id) one short transaction per page so
// heap stays flat and the write lock is never held for the whole table.
// A mid-run crash leaves committed pages on disk; rerun converges. It
// returns the total rows deleted.
func Purge(app core.App) (int, error) {
	cutoff := cutoffFor(time.Now(), GetConfig().MaxDays)
	cutoffParam := cutoff.Format(types.DefaultDateLayout)
	n := 0
	var cursorVal, cursorID string
	hasCursor := false
	for {
		params := dbx.Params{"cutoff": cutoffParam}
		filter := "created < {:cutoff}"
		if hasCursor {
			params["cursor"] = cursorVal
			params["cursorID"] = cursorID
			filter += " && (created > {:cursor} || (created = {:cursor} && id > {:cursorID}))"
		}
		page, err := app.FindRecordsByFilter(logsCollection, filter, "created,id", purgePageSize, 0, params)
		if err != nil {
			return n, err
		}
		if len(page) == 0 {
			break
		}
		last := page[len(page)-1]
		cursorVal = last.GetDateTime("created").Time().UTC().Format(types.DefaultDateLayout)
		cursorID = last.Id
		hasCursor = true
		if err := app.RunInTransaction(func(txApp core.App) error {
			for _, r := range page {
				if err := txApp.Delete(r); err != nil {
					return err
				}
				n++
			}
			return nil
		}); err != nil {
			return n, err
		}
		if len(page) < purgePageSize {
			break
		}
	}
	return n, nil
}

// queryPageSize caps one viewer page.
const queryPageSize = 500

// Query lists server_logs rows newest-first for the future viewer
// endpoint. level < 0 matches any level; empty src matches any src.
// page starts at 1, perPage clamps to 1..500 (default 50 on invalid).
func Query(app core.App, level int, src string, page, perPage int) ([]*core.Record, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > queryPageSize {
		perPage = 50
	}
	var filters []string
	params := dbx.Params{}
	if level >= 0 {
		filters = append(filters, "level = {:level}")
		params["level"] = level
	}
	if strings.TrimSpace(src) != "" {
		filters = append(filters, "src = {:src}")
		params["src"] = src
	}
	filter := strings.Join(filters, " && ")
	return app.FindRecordsByFilter(logsCollection, filter, "-created", perPage, (page-1)*perPage, params)
}
