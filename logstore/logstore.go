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

// Defaults mirror the intended operator history window: store on, 30d,
// success lines on.
const (
	DefaultEnabled = true
	DefaultMaxDays = 30

	// DefaultSuccessLogs keeps success lines (served/cached/accepted)
	// in the store. When false, only rejections reach server_logs
	// (stderr keeps every line either way).
	DefaultSuccessLogs = true

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
	Enabled     bool `json:"enabled"`
	MaxDays     int  `json:"maxDays"`
	SuccessLogs bool `json:"successLogs"`
}

// Defaults returns the code defaults (before env/DB overrides).
func Defaults() Settings {
	return Settings{
		Enabled:     DefaultEnabled,
		MaxDays:     DefaultMaxDays,
		SuccessLogs: DefaultSuccessLogs,
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
	c.SuccessLogs = parseEnvBool("CONFIGWIRE_LOG_STORE_SUCCESS_LOGS", c.SuccessLogs)
	return c
}

// ValidateSettings enforces 1..1825 on maxDays (enabled/successLogs are
// plain bools, always valid). Violations are 400-style settingsErrors
// for the future admin PUT path.
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
		rec.Set("successLogs", c.SuccessLogs)
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
	// successLogs defaults true, and GetBool cannot tell an explicit
	// false from a missing (pre-migration) field — so only a collection
	// that actually carries the field overrides the default. The
	// migration backfills the row, making this a stale-schema guard.
	if collection.Fields.GetByName("successLogs") != nil {
		c.SuccessLogs = rec.GetBool("successLogs")
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
	rec.Set("successLogs", c.SuccessLogs)
	return app.Save(rec)
}

// Entry is one persisted log line: the greppable message plus the
// dedicated filter columns populated alongside it (see migration
// 1790000026). Status 0 means absent (left unset); empty strings stay
// unset. Env/Project hold the live record IDs resolved at write time
// (never slugs parsed from messages). Callers keep building the exact
// same single-space k=v message string as before — the columns are a
// second, indexed copy, never a replacement.
type Entry struct {
	Level                                                  int
	Src, Message, Project, Env, KeyPrefix, Reason, Limiter string
	Status                                                 int
}

// Write persists one reason-code line to server_logs. It no-ops (nil)
// when the store is disabled, returns an error on empty message or
// out-of-range level, and otherwise performs a single Save — callers must
// treat the error as best-effort (log to stderr, never retry-block the
// request). Src defaults to "configwire" when blank. level/message/src
// plus the filter columns only: callers must pass reason codes, never PII.
func Write(app core.App, e Entry) error {
	if !GetConfig().Enabled {
		return nil
	}
	if e.Level < minLevel || e.Level > maxLevel {
		return &settingsError{msg: "invalid level: must be 0..3."}
	}
	if strings.TrimSpace(e.Message) == "" {
		return errors.New("logstore: message is required")
	}
	src := e.Src
	if strings.TrimSpace(src) == "" {
		src = DefaultSrc
	}
	collection, err := app.FindCollectionByNameOrId(logsCollection)
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	rec.Set("level", e.Level)
	rec.Set("src", src)
	rec.Set("message", e.Message)
	if e.Project != "" {
		rec.Set("project", e.Project)
	}
	if e.Env != "" {
		rec.Set("env", e.Env)
	}
	if e.KeyPrefix != "" {
		rec.Set("key_prefix", e.KeyPrefix)
	}
	if e.Status != 0 {
		rec.Set("status", e.Status)
	}
	if e.Reason != "" {
		rec.Set("reason", e.Reason)
	}
	if e.Limiter != "" {
		rec.Set("limiter", e.Limiter)
	}
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

// LogQuery carries the viewer filters for Query. Empty strings match
// anything; Level < 0 matches any level. Project/Env/Key/Status/Reason/
// Limiter match the dedicated filter columns (see Entry); the API layer
// validates the text ones to [A-Za-z0-9_-] and status to all-digits.
type LogQuery struct {
	Level                                           int
	Src, Project, Env, Key, Status, Reason, Limiter string
}

// conditions builds the indexed equality conditions for the set fields,
// shared by Query's page and the total-count query so they cannot drift.
// Level/src equality is unchanged; the new columns filter by equality
// only when set. Status parses to int (unparseable status can never equal
// an int column, so it binds -1 and matches nothing); the API layer
// already rejects non-digit status with 400, so this is a backstop for
// direct callers. Callers join with their own conjunction: " && " for the
// PocketBase filter syntax (FindRecordsByFilter) or " AND " for raw SQL
// (dbx.NewExp in the count query).
func (q LogQuery) conditions() ([]string, dbx.Params) {
	var conds []string
	params := dbx.Params{}
	if q.Level >= 0 {
		conds = append(conds, "level = {:level}")
		params["level"] = q.Level
	}
	if strings.TrimSpace(q.Src) != "" {
		conds = append(conds, "src = {:src}")
		params["src"] = q.Src
	}
	if q.Project != "" {
		conds = append(conds, "project = {:project}")
		params["project"] = q.Project
	}
	if q.Env != "" {
		conds = append(conds, "env = {:env}")
		params["env"] = q.Env
	}
	if q.Key != "" {
		conds = append(conds, "key_prefix = {:key}")
		params["key"] = q.Key
	}
	if q.Status != "" {
		n, err := strconv.Atoi(q.Status)
		if err != nil {
			n = -1
		}
		conds = append(conds, "status = {:status}")
		params["status"] = n
	}
	if q.Reason != "" {
		conds = append(conds, "reason = {:reason}")
		params["reason"] = q.Reason
	}
	if q.Limiter != "" {
		conds = append(conds, "limiter = {:limiter}")
		params["limiter"] = q.Limiter
	}
	return conds, params
}

// Query lists server_logs rows newest-first for the viewer endpoint.
// page starts at 1, perPage clamps to 1..500 (default 50 on invalid).
func Query(app core.App, q LogQuery, page, perPage int) ([]*core.Record, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > queryPageSize {
		perPage = 50
	}
	conds, params := q.conditions()
	return app.FindRecordsByFilter(logsCollection, strings.Join(conds, " && "), "-created", perPage, (page-1)*perPage, params)
}
