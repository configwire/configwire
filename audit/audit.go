// Package audit owns the durable, default-on admin audit trail.
//
// Model: one append-only row per sensitive admin action in the dedicated
// audit_log collection (migration 1790000029). Rows carry the acting
// superuser (actor/actorId), the stable action name, the target
// (targetType/targetId/targetLabel), an HTTP-like status and a small
// JSON detail map. Settings are env-only in v1:
//
//	CONFIGWIRE_AUDIT_ENABLED    default true
//	CONFIGWIRE_AUDIT_MAX_DAYS   default 365 (clamped to 1..1825)
//
// Writes are best-effort by design: a disabled trail is a no-op, a save
// failure is logged to stderr by the Record helpers and NEVER fails the
// business request. Callers must never put secrets, tokens, tickets, full
// SDK keys, full allowlists, or PII beyond the actor email / target label
// into Entry or Detail.
//
// Retention mirrors logstore: rows older than now-MaxDays die page by
// page (one short transaction per page, ordered by (created, id)), wired
// into the purge ticker and the manual purge route.
package audit

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

// Defaults: audit on by default, one year of history.
const (
	DefaultEnabled = true
	DefaultMaxDays = 365

	// MinDays / MaxDays bound the retention knob (1d..1825d = 5yr max,
	// same as retention/logstore).
	MinDays = 1
	MaxDays = 1825

	// collectionName is the PocketBase collection holding audit rows.
	collectionName = "audit_log"

	// actorSystem is the actor used when no authenticated superuser is
	// present (boot jobs, public login failures, SSO redeem).
	actorSystem = "system"

	// defaultPerPage / maxPerPage bound one result page.
	defaultPerPage = 50
	maxPerPage     = 200

	// purgePageSize bounds one delete page: one page = one short
	// transaction.
	purgePageSize = 500
)

// Settings carries the env-only audit policy.
type Settings struct {
	Enabled bool `json:"enabled"`
	MaxDays int  `json:"maxDays"`
}

// Defaults returns the code defaults (before env overrides).
func Defaults() Settings {
	return Settings{Enabled: DefaultEnabled, MaxDays: DefaultMaxDays}
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

// loadDefaultsFromEnv applies the CONFIGWIRE_AUDIT_* overrides over
// Defaults. Invalid values fall back to the default; out-of-range
// maxDays values are clamped by clampSettings.
func loadDefaultsFromEnv() Settings {
	c := Defaults()
	c.Enabled = parseEnvBool("CONFIGWIRE_AUDIT_ENABLED", c.Enabled)
	c.MaxDays = parseEnvInt("CONFIGWIRE_AUDIT_MAX_DAYS", c.MaxDays)
	return clampSettings(c)
}

// clampSettings forces maxDays into MinDays..MaxDays (used when loading
// env values that bypass admin validation), falling back to the code
// default on out-of-range values.
func clampSettings(c Settings) Settings {
	if c.MaxDays < MinDays || c.MaxDays > MaxDays {
		c.MaxDays = DefaultMaxDays
	}
	return c
}

// Module state: atomic settings under RWMutex (mirrors logstore/retention).
var (
	cfgMu   sync.RWMutex
	current = loadDefaultsFromEnv()
)

// GetConfig returns a snapshot of the active settings.
func GetConfig() Settings {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return current
}

// resetForTest replaces settings (tests only, same package).
func resetForTest(c Settings) {
	cfgMu.Lock()
	current = clampSettings(c)
	cfgMu.Unlock()
}

// Entry is one audit row. Actor defaults to "system" and Status 0 stays
// unset. Detail must stay small and non-secret.
type Entry struct {
	Actor       string
	ActorID     string
	Action      string
	TargetType  string
	TargetID    string
	TargetLabel string
	Status      int
	Detail      map[string]any
}

// Write persists one audit row. It no-ops (nil) when the trail is
// disabled and returns an error on a missing action. Callers go through
// Record/RecordApp so write errors are logged and ignored.
func Write(app core.App, e Entry) error {
	if !GetConfig().Enabled {
		return nil
	}
	action := strings.TrimSpace(e.Action)
	if action == "" {
		return errors.New("audit: action is required")
	}
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	actor := strings.TrimSpace(e.Actor)
	if actor == "" {
		actor = actorSystem
	}
	rec.Set("actor", actor)
	if e.ActorID != "" {
		rec.Set("actorId", e.ActorID)
	}
	rec.Set("action", action)
	if e.TargetType != "" {
		rec.Set("targetType", e.TargetType)
	}
	if e.TargetID != "" {
		rec.Set("targetId", e.TargetID)
	}
	if e.TargetLabel != "" {
		rec.Set("targetLabel", e.TargetLabel)
	}
	if e.Status != 0 {
		rec.Set("status", e.Status)
	}
	if len(e.Detail) > 0 {
		rec.Set("detail", e.Detail)
	}
	return app.Save(rec)
}

// actorFromRecord maps an auth record to (actor, actorId). Absent auth
// yields ("system", ""), so boot and public actions are attributable.
func actorFromRecord(rec *core.Record) (string, string) {
	if rec == nil {
		return actorSystem, ""
	}
	if email := strings.TrimSpace(rec.GetString("email")); email != "" {
		return email, rec.Id
	}
	if rec.Id != "" {
		return rec.Id, rec.Id
	}
	return actorSystem, ""
}

// ActorFromRequest returns the acting superuser's email and record id
// from re.Auth ("system"/"" when absent).
func ActorFromRequest(re *core.RequestEvent) (email, id string) {
	if re == nil {
		return actorSystem, ""
	}
	return actorFromRecord(re.Auth)
}

// ActorFromRequestInfo is the hook-side variant for e.RequestInfo().
func ActorFromRequestInfo(info *core.RequestInfo) (email, id string) {
	if info == nil {
		return actorSystem, ""
	}
	return actorFromRecord(info.Auth)
}

// Record fills the actor from re.Auth (when Entry does not name one),
// writes the row and logs (never returns) a failure.
func Record(re *core.RequestEvent, e Entry) {
	email, id := ActorFromRequest(re)
	if strings.TrimSpace(e.Actor) == "" {
		e.Actor = email
	}
	if e.ActorID == "" {
		e.ActorID = id
	}
	RecordApp(re.App, e)
}

// RecordApp writes the row and logs (never returns) a failure. Hooks use
// it after e.Next() succeeds; the actor must already be on the Entry.
func RecordApp(app core.App, e Entry) {
	if err := Write(app, e); err != nil {
		log.Printf("audit: write %s failed: %v", e.Action, err)
	}
}

// Filter carries the query filters. Empty strings match anything,
// Status 0 matches any status, SinceDays 0 means no time bound.
type Filter struct {
	Action     string
	Actor      string
	TargetType string
	TargetID   string
	Status     int
	SinceDays  int
	Page       int
	PerPage    int
}

// conditions builds the indexed equality conditions plus the optional
// time bound. Shared by Query's page and the total-count query so they
// cannot drift. Callers join with " && " (FindRecordsByFilter) or
// " AND " (dbx.NewExp).
func (q Filter) conditions(now time.Time) ([]string, dbx.Params) {
	var conds []string
	params := dbx.Params{}
	if v := strings.TrimSpace(q.Action); v != "" {
		conds = append(conds, "action = {:action}")
		params["action"] = v
	}
	if v := strings.TrimSpace(q.Actor); v != "" {
		conds = append(conds, "actor = {:actor}")
		params["actor"] = v
	}
	if v := strings.TrimSpace(q.TargetType); v != "" {
		conds = append(conds, "targetType = {:targetType}")
		params["targetType"] = v
	}
	if v := strings.TrimSpace(q.TargetID); v != "" {
		conds = append(conds, "targetId = {:targetId}")
		params["targetId"] = v
	}
	if q.Status != 0 {
		conds = append(conds, "status = {:status}")
		params["status"] = q.Status
	}
	if q.SinceDays > 0 {
		since := now.UTC().AddDate(0, 0, -q.SinceDays).Truncate(time.Millisecond)
		conds = append(conds, "created >= {:since}")
		params["since"] = since.Format(types.DefaultDateLayout)
	}
	return conds, params
}

// Query lists audit rows newest-first with the total matching count.
// Page starts at 1; perPage clamps to 1..200 (default 50).
func Query(app core.App, q Filter) (items []*core.Record, total int, err error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	perPage := q.PerPage
	if perPage < 1 || perPage > maxPerPage {
		perPage = defaultPerPage
	}
	conds, params := q.conditions(time.Now())
	items, err = app.FindRecordsByFilter(
		collectionName,
		strings.Join(conds, " && "),
		"-created",
		perPage,
		(page-1)*perPage,
		params,
	)
	if err != nil {
		return nil, 0, err
	}
	var exprs []dbx.Expression
	if len(conds) > 0 {
		exprs = append(exprs, dbx.NewExp(strings.Join(conds, " AND "), params))
	}
	n, err := app.CountRecords(collectionName, exprs...)
	if err != nil {
		return nil, 0, err
	}
	return items, int(n), nil
}

// cutoffFor maps now to the purge horizon (now minus maxDays).
func cutoffFor(now time.Time, maxDays int) time.Time {
	return now.UTC().AddDate(0, 0, -maxDays).Truncate(time.Millisecond)
}

// pageFilter builds the doomed-rows filter for one audit page: created
// strictly before cutoff, plus the (created,id) keyset cursor when paging
// past the first page. Shared by Purge and Count so they cannot drift.
func pageFilter(hasCursor bool, cursorVal, cursorID, cutoffParam string) (string, dbx.Params) {
	params := dbx.Params{"cutoff": cutoffParam}
	filter := "created < {:cutoff}"
	if hasCursor {
		params["cursor"] = cursorVal
		params["cursorID"] = cursorID
		filter += " && (created > {:cursor} || (created = {:cursor} && id > {:cursorID}))"
	}
	return filter, params
}

// Count reports how many audit rows Purge would delete under the live
// MaxDays window, with zero writes (dry-run path for the manual purge
// route). It walks the same indexed candidate set with the same
// (created,id) cursor paging as Purge. Retention applies regardless of
// the Enabled flag (stored history ages out even when new writes are off).
func Count(app core.App) (int, error) {
	cutoff := cutoffFor(time.Now(), GetConfig().MaxDays)
	cutoffParam := cutoff.Format(types.DefaultDateLayout)
	n := 0
	var cursorVal, cursorID string
	hasCursor := false
	for {
		filter, params := pageFilter(hasCursor, cursorVal, cursorID, cutoffParam)
		page, err := app.FindRecordsByFilter(collectionName, filter, "created,id", purgePageSize, 0, params)
		if err != nil {
			return n, err
		}
		if len(page) == 0 {
			break
		}
		n += len(page)
		last := page[len(page)-1]
		cursorVal = last.GetDateTime("created").Time().UTC().Format(types.DefaultDateLayout)
		cursorID = last.Id
		hasCursor = true
		if len(page) < purgePageSize {
			break
		}
	}
	return n, nil
}

// Purge deletes audit rows with created strictly older than now-MaxDays,
// paged by (created, id) one short transaction per page so heap stays
// flat and the write lock is never held for the whole table. A mid-run
// crash leaves committed pages on disk; rerun converges. It returns the
// total rows deleted.
func Purge(app core.App) (int, error) {
	cutoff := cutoffFor(time.Now(), GetConfig().MaxDays)
	cutoffParam := cutoff.Format(types.DefaultDateLayout)
	n := 0
	var cursorVal, cursorID string
	hasCursor := false
	for {
		filter, params := pageFilter(hasCursor, cursorVal, cursorID, cutoffParam)
		page, err := app.FindRecordsByFilter(collectionName, filter, "created,id", purgePageSize, 0, params)
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
