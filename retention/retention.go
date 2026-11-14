// Package retention owns the tunable compliance-retention window.
// mirroring configwire/limits (rate_settings singleton pattern).
//
// Model: operators set a single "Event history retention (days)" value.
// Internally raw events live up to 30 days by default and daily aggregates
// cover any additional days beyond the raw window:
//
//	rawDays    = min(30, retentionDays) (single-knob derivation)
//	rollupDays = retentionDays
//
// The legacy {rawDays, rollupDays} pair is an advanced override: a valid
// rawDays < 30 shortens the raw window (EffectiveRawDays honors it) and
// rollups cover rawDays..retentionDays.
//
// When total retention <= the active raw window, event_daily is intentionally
// empty and all reads are raw-only. The invariant RollupDays >= RawDays
// always holds.
// Tunables live in Config, defaulted from code ({30,90} so the historic
// 30d raw / 90d total defaults are preserved), overridden by env,
// overridden at runtime by the retention_settings collection row
// (key=global) via the admin API.
//
// Why a singleton row: one operator-chosen policy per binary, same as
// limits.rate_settings — find-or-create on key="global", backed by a
// UNIQUE index on key. Purge cutoffs read from GetConfig().
package retention

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// Defaults preserve the historic 30d raw / 90d total retention.
const (
	DefaultRawDays    = 30
	DefaultRollupDays = 90
	// MaxRawDays is the hard cap on the raw-event window. It is also the
	// threshold above which daily rollups begin; retentionDays <= MaxRawDays
	// means raw-only storage.
	MaxRawDays = 30

	// MinDays / MaxDays bound each knob (1d..1825d = 5yr max per Q4).
	MinDays = 1
	MaxDays = 1825
)

// Config carries the tunable retention windows in days.
// RollupDays is the user-facing total retention; RawDays is the advanced
// override for the raw-event window, honored when valid and always
// <= min(MaxRawDays, RollupDays). Single-knob writes derive it as
// min(RollupDays, MaxRawDays).
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

// RetentionDays returns the user-facing total retention window.
func (c Config) RetentionDays() int {
	return c.RollupDays
}

// EffectiveRawDays returns the active raw-event window: the stored RawDays
// override when valid (1..MaxRawDays and <= RollupDays), otherwise derived
// as min(RollupDays, MaxRawDays). It is never larger than MaxRawDays and
// never larger than RollupDays; invalid totals fall back to MaxRawDays so
// callers never compute a zero/negative cutoff.
func (c Config) EffectiveRawDays() int {
	if c.RawDays >= MinDays && c.RawDays <= MaxRawDays && c.RawDays <= c.RollupDays {
		return c.RawDays
	}
	if c.RollupDays < MinDays {
		return MaxRawDays
	}
	if c.RollupDays < MaxRawDays {
		return c.RollupDays
	}
	return MaxRawDays
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
//
// CONFIGWIRE_RETENTION_DAYS is the single user-facing knob. The legacy
// CONFIGWIRE_RAW_RETENTION_DAYS / CONFIGWIRE_ROLLUP_RETENTION_DAYS envs
// still work as advanced overrides; when present, rawDays is capped at
// MaxRawDays and rollupDays is treated as the total retention. A
// set-but-unparseable single knob falls through to the legacy pair (a
// valid single knob always wins and the legacy pair is ignored).
func loadDefaultsFromEnv() Config {
	c := Defaults()
	if v := os.Getenv("CONFIGWIRE_RETENTION_DAYS"); v != "" {
		// A set-but-unparseable single knob falls through to the legacy
		// env pair below instead of silently discarding it: an operator
		// migrating who typos the new var keeps their legacy tuning
		// (out-of-range values are clamped by clampConfig either way).
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.RollupDays = n
			c.RawDays = min(n, MaxRawDays)
			warnCeiling()
			return c
		}
	}
	c.RollupDays = parseEnvInt("CONFIGWIRE_ROLLUP_RETENTION_DAYS", c.RollupDays)
	c.RawDays = min(parseEnvInt("CONFIGWIRE_RAW_RETENTION_DAYS", min(c.RollupDays, MaxRawDays)), c.RollupDays, MaxRawDays)
	warnCeiling()
	return c
}

// maxRetentionDaysEnvName is the plan-ceiling knob (tenant tier
// enforcement, injected by the orchestrator fragment): PUT values and
// env/DB loads above it are rejected/clamped. Unset/empty/malformed env
// means "no ceiling" (falls back to MaxDays, standalone unchanged).
const maxRetentionDaysEnvName = "CONFIGWIRE_MAX_RETENTION_DAYS"

// CeilingDays caps RawDays and RollupDays (CONFIGWIRE_MAX_RETENTION_DAYS).
func CeilingDays() int {
	v := strings.TrimSpace(os.Getenv(maxRetentionDaysEnvName))
	if v == "" {
		return MaxDays
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return MaxDays
	}
	return n
}

// warnCeiling logs when the plan ceiling sits below MinDays (misconfig:
// every value above it will be rejected). Called from the env-load path,
// never from per-request paths.
func warnCeiling() {
	if c := CeilingDays(); c < MinDays {
		log.Printf("retention: %s=%d below minimum %d — values above it will be rejected", maxRetentionDaysEnvName, c, MinDays)
	}
}

// ValidateConfig enforces 1..1825 on each knob narrowed by the plan
// ceiling (1..ceiling) plus the new raw-window cap (rawDays must be
// <= MaxRawDays and <= rollupDays). Above-ceiling values name the
// ceiling, e.g. "invalid rollupDays: must be 1..7 (plan ceiling).";
// without a ceiling env the bound is 1..1825 exactly as before.
// Violations are 400-style configErrors for the admin PUT path.
func ValidateConfig(c Config) error {
	ceiling := CeilingDays()
	if c.RawDays < MinDays || c.RawDays > MaxDays {
		return &configError{msg: "invalid rawDays: must be 1..1825."}
	}
	if c.RawDays > ceiling {
		return &configError{msg: "invalid rawDays: must be 1.." + strconv.Itoa(ceiling) + " (plan ceiling)."}
	}
	if c.RawDays > MaxRawDays {
		return &configError{msg: "invalid rawDays: must be at most " + strconv.Itoa(MaxRawDays) + "."}
	}
	if c.RollupDays < MinDays || c.RollupDays > MaxDays {
		return &configError{msg: "invalid rollupDays: must be 1..1825."}
	}
	if c.RollupDays > ceiling {
		return &configError{msg: "invalid rollupDays: must be 1.." + strconv.Itoa(ceiling) + " (plan ceiling)."}
	}
	if c.RawDays > c.RollupDays {
		return &configError{msg: "invalid rawDays: must be <= rollupDays."}
	}
	return nil
}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

// clampConfig forces rollupDays into MinDays..ceiling, preserves a valid
// rawDays override (capped at min(rollupDays, MaxRawDays)), and falls back
// to the code default on out-of-range absolute values. Used when loading
// env/DB values that bypass admin validation. Without a ceiling env the
// ceiling is MaxDays, so the standalone behavior is unchanged.
func clampConfig(c Config) Config {
	d := Defaults()
	ceiling := CeilingDays()
	clamp := func(v, def int) int {
		if v < MinDays || v > MaxDays {
			return def
		}
		if v > ceiling {
			if ceiling < MinDays {
				return def
			}
			return ceiling
		}
		return v
	}
	c.RollupDays = clamp(c.RollupDays, d.RollupDays)
	c.RawDays = min(clamp(c.RawDays, d.RawDays), c.RollupDays, MaxRawDays)
	return c
}

// Module state: atomic config under RWMutex (mirrors limits.current).
var (
	cfgMu   sync.RWMutex
	current = clampConfig(loadDefaultsFromEnv())
)

// ResetForTest replaces config (tests only, cross-package use allowed).
func ResetForTest(c Config) {
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
