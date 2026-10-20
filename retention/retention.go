// Package retention owns the tunable compliance-retention windows,
// mirroring configwire/limits (rate_settings singleton pattern) without
// any API/UI yet (Wave 3 wires the ticker/handler).
//
// Model: raw events live RawDays, daily aggregates live RollupDays, with
// the invariant RollupDays >= RawDays (rollups must outlive the raw rows
// they were built from). Tunables live in Config, defaulted from code
// ({30,90} matching purge.RawRetentionDays/RollupRetentionDays),
// overridden by env, overridden at runtime by the retention_settings
// collection row (key=global) via a future admin API.
//
// Why a singleton row: one operator-chosen policy per binary, same as
// limits.rate_settings — find-or-create on key="global", backed by a
// UNIQUE index on key. Wave 3 will rewire purge cutoffs to GetConfig()
// (purge.go cutoffs stay the source of truth until then — do NOT rewire
// here).
package retention

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Defaults mirror purge.RawRetentionDays / purge.RollupRetentionDays:
// raw events 30d, daily aggregates 90d.
const (
	DefaultRawDays    = 30
	DefaultRollupDays = 90

	// MinDays / MaxDays bound each knob (1d..1825d = 5yr max per Q4).
	MinDays = 1
	MaxDays = 1825
)

// Config carries the tunable retention windows in days.
type Config struct {
	RawDays    int `json:"rawDays"`
	RollupDays int `json:"rollupDays"`
}

// Defaults returns the code defaults (before env/DB overrides).
func Defaults() Config {
	return Config{
		RawDays:    DefaultRawDays,
		RollupDays: DefaultRollupDays,
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

// loadDefaultsFromEnv applies the CONFIGWIRE_* overrides over Defaults.
// Invalid (non-numeric) values fall back to the default; out-of-range env
// values are clamped by clampConfig at package init and again in
// ensureLoaded on the serve path.
func loadDefaultsFromEnv() Config {
	c := Defaults()
	c.RawDays = parseEnvInt("CONFIGWIRE_RAW_RETENTION_DAYS", c.RawDays)
	c.RollupDays = parseEnvInt("CONFIGWIRE_ROLLUP_RETENTION_DAYS", c.RollupDays)
	return c
}

// ValidateConfig enforces 1..1825 on each knob plus the rollup >= raw
// invariant (checked in struct order so multi-field failures always
// report rawDays first). Violations are 400-style configErrors for the
// future admin PUT path.
func ValidateConfig(c Config) error {
	if c.RawDays < MinDays || c.RawDays > MaxDays {
		return &configError{msg: "invalid rawDays: must be 1..1825."}
	}
	if c.RollupDays < MinDays || c.RollupDays > MaxDays {
		return &configError{msg: "invalid rollupDays: must be 1..1825."}
	}
	if c.RollupDays < c.RawDays {
		return &configError{msg: "invalid rollupDays: must be >= rawDays."}
	}
	return nil
}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

// clampConfig forces each knob into 1..1825 (used when loading env/DB
// values that bypass admin validation), falling back to the code default
// per knob, then repairs the invariant by raising rollupDays up to
// rawDays (rollups must outlive the raw rows they aggregate).
func clampConfig(c Config) Config {
	d := Defaults()
	if c.RawDays < MinDays || c.RawDays > MaxDays {
		c.RawDays = d.RawDays
	}
	if c.RollupDays < MinDays || c.RollupDays > MaxDays {
		c.RollupDays = d.RollupDays
	}
	if c.RollupDays < c.RawDays {
		c.RollupDays = c.RawDays
	}
	return c
}

// Module state: atomic config under RWMutex (mirrors limits.current).
var (
	cfgMu   sync.RWMutex
	current = clampConfig(loadDefaultsFromEnv())
)

// resetForTest replaces config (tests only, same package).
func resetForTest(c Config) {
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
}

// GetConfig returns a snapshot of the active config.
func GetConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return current
}

// applyConfig validates and installs c (future admin PUT path).
func applyConfig(c Config) error {
	if err := ValidateConfig(c); err != nil {
		return err
	}
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
	return nil
}

// collectionName is the PocketBase collection holding the singleton row.
const collectionName = "retention_settings"

// globalKey identifies the singleton settings row.
const globalKey = "global"

// ensureLoaded seeds the singleton retention_settings row when absent and
// loads DB values over env defaults. Missing collection (migration not yet
// applied) keeps env defaults with a warning — never fatal on serve.
func ensureLoaded(app core.App) {
	c := loadDefaultsFromEnv()
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		cfgMu.Lock()
		current = clampConfig(c)
		cfgMu.Unlock()
		log.Print("retention: retention_settings collection missing, using env defaults")
		return
	}
	rec, err := findGlobalRow(app)
	if err != nil {
		log.Printf("retention: failed to read retention_settings: %v, using env defaults", err)
		cfgMu.Lock()
		current = clampConfig(c)
		cfgMu.Unlock()
		return
	}
	if rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("key", globalKey)
		rec.Set("rawDays", c.RawDays)
		rec.Set("rollupDays", c.RollupDays)
		if err := app.Save(rec); err != nil {
			log.Printf("retention: failed to seed retention_settings: %v", err)
		}
		cfgMu.Lock()
		current = clampConfig(c)
		cfgMu.Unlock()
		return
	}
	if v := rec.GetInt("rawDays"); v > 0 {
		c.RawDays = v
	}
	if v := rec.GetInt("rollupDays"); v > 0 {
		c.RollupDays = v
	}
	cfgMu.Lock()
	current = clampConfig(c)
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

// upsertGlobalRow writes the singleton retention_settings row
// (key=global), creating it when absent. Uses the request-scoped app
// (never captured).
func upsertGlobalRow(app core.App, c Config) error {
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
	rec.Set("rawDays", c.RawDays)
	rec.Set("rollupDays", c.RollupDays)
	return app.Save(rec)
}
