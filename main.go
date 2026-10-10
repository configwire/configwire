package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"

	"github.com/configwire/configwire/account"
	"github.com/configwire/configwire/envresolve"
	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/fetch"
	"github.com/configwire/configwire/ingest"
	"github.com/configwire/configwire/limits"
	_ "github.com/configwire/configwire/migrations"
	"github.com/configwire/configwire/purge"
	"github.com/configwire/configwire/releases"
	"github.com/configwire/configwire/security"
	"github.com/configwire/configwire/stats"
	"github.com/configwire/configwire/stream"
)

// ConfigWire data-integrity hooks.
// Flag key shape + per-project cap and releases immutability live in
// code (not collection options) so they apply to every write path
// (API, dashboard, server-side e.App saves).

//go:embed VERSION
var embeddedVersion string

// appVersion normalizes the embedded VERSION file (single source of
// truth) to a "v" prefix with trimmed whitespace.
// Falls back to "vdev" only when the embedded VERSION file is empty.
func appVersion() string {
	v := strings.TrimSpace(embeddedVersion)
	if v == "" {
		return "vdev"
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// staticDigest fingerprints the Admin UI shell (index.html, styles.css,
// js/*.js) so the browser can detect a redeploy without a VERSION bump.
// Computed once at serve start; "" when pb_public is unreachable
// (e.g. wrong cwd — the client then skips the asset comparison).
func staticDigest() string {
	files := []string{"./pb_public/index.html", "./pb_public/styles.css"}
	if entries, err := os.ReadDir("./pb_public/js"); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
				files = append(files, "./pb_public/js/"+e.Name())
			}
		}
	}
	sort.Strings(files)
	h := sha256.New()
	ok := false
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		ok = true
		h.Write([]byte(f))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	if !ok {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// setStaticCacheHeaders assigns cache policy to Admin UI static responses
// so no manual ?v= query bump is ever needed:
//   - "/" and *.html (incl. extensionless SPA-fallback paths that serve
//     index.html) -> no-store: the entry point is always fresh.
//   - *.js/*.css -> no-cache: revalidated via Last-Modified (304 when
//     unchanged), so updates apply on next load with no version query.
//   - images/fonts -> 1h public cache (non-critical bytes).
//
// API routes are untouched.
func setStaticCacheHeaders(re *core.RequestEvent) {
	p := re.Request.URL.Path
	if strings.HasPrefix(p, "/api/") {
		return
	}
	h := re.Response.Header()
	switch {
	case p == "/" || strings.HasSuffix(p, ".html"):
		h.Set("Cache-Control", "no-store")
	case strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css"):
		h.Set("Cache-Control", "no-cache")
	case strings.HasSuffix(p, ".png") || strings.HasSuffix(p, ".jpg") ||
		strings.HasSuffix(p, ".jpeg") || strings.HasSuffix(p, ".svg") ||
		strings.HasSuffix(p, ".ico") || strings.HasSuffix(p, ".woff2"):
		h.Set("Cache-Control", "public, max-age=3600")
	default:
		h.Set("Cache-Control", "no-store")
	}
}

var flagKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

const (
	maxFlagKeyLength   = 128
	maxFlagsPerProject = 1000
)

func checkFlagKey(key string) error {
	if len(key) == 0 || len(key) > maxFlagKeyLength || !flagKeyPattern.MatchString(key) {
		return errors.New("invalid flag key: must match ^[A-Za-z_][A-Za-z0-9_.-]*$ and be 1-128 chars")
	}
	return nil
}

// checkRuleRegexConditions validates regex conditions at rule write time
// (data API, dashboard, server-side saves) so an un-compilable or
// oversized pattern is rejected with a 400 at save. Without it the bad
// pattern would persist and silently never match at fetch (eval treats an
// invalid regex as false), wasting one compile per fetch until the next
// publish. The condition is either a single object or an array of
// objects; releases.validateCondition re-checks compilability at publish
// for rows predating this hook.
func checkRuleRegexConditions(cond any) error {
	if cond == nil {
		return nil
	}
	raw, err := json.Marshal(cond)
	if err != nil {
		return apis.NewBadRequestError("rule condition must be a JSON object or array of objects", nil)
	}
	var objs []map[string]any
	if err := json.Unmarshal(raw, &objs); err != nil {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil || m == nil {
			return apis.NewBadRequestError("rule condition must be a JSON object or array of objects", nil)
		}
		objs = []map[string]any{m}
	}
	for _, m := range objs {
		op, _ := m["op"].(string)
		if op != "regex" {
			continue
		}
		pattern, ok := m["value"].(string)
		if !ok {
			continue
		}
		if err := eval.ValidateRegexPattern(pattern); err != nil {
			return apis.NewBadRequestError("invalid regex condition: "+err.Error(), nil)
		}
	}
	return nil
}

// envDeleteMu serializes environment deletes so two concurrent deletes
// of a project's last two environments cannot both pass the count check.
var envDeleteMu sync.Mutex

// envSlugMu serializes environment creates/updates so two concurrent
// creates with the same (project, slug) cannot both pass the uniqueness
// check before either row is saved (check-then-act race). Held across
// e.Next() like envDeleteMu so check and save are atomic together; env
// writes are rare admin ops, so the serialization cost is negligible.
var envSlugMu sync.Mutex

// Exact count-query equivalent:
//
//	SELECT COUNT(*) FROM flags WHERE project = '<projectId>'
//
// Filtered COUNT(*) at the DB layer (no full-table scan by design).
func countProjectFlags(app core.App, project string) (int64, error) {
	return app.CountRecords("flags", dbx.HashExp{"project": project})
}

// checkFlagKeyTaken rejects a duplicate (key, project) flag: flag keys
// are snapshot identity downstream (envresolve.MatchFlag is
// first-match-wins), so two rows with the same key make evaluation
// order-dependent. Same key in a different project stays allowed.
// O(n) scan like checkEnvSlug; flag writes are rare admin ops.
func checkFlagKeyTaken(app core.App, rec *core.Record) error {
	recs, err := app.FindAllRecords("flags")
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.Id == rec.Id {
			continue
		}
		if r.GetString("project") == rec.GetString("project") &&
			r.GetString("key") == rec.GetString("key") {
			return apis.NewBadRequestError("flag key already exists for this project", nil)
		}
	}
	return nil
}

func registerConfigwireHooks(app core.App) {
	// Superusers: the last remaining admin cannot be deleted on any
	// path (custom account endpoint, data API, dashboard, server
	// saves). The hook is authoritative; deleteAccount keeps a
	// fast-path check for its 404 -> 400 -> 200 ordering.
	account.RegisterGuard(app)

	// Releases are immutable and publish-only: direct data-API writes are
	// rejected; only publish/rollback handler saves (marked via a context
	// value, see releases.IsInternalSaveCtx) pass the create/delete hooks. Updates
	// are always denied: rollback republishes the old snapshot as a NEW row.
	releasesImmutable := errors.New("releases are immutable: publish a new release instead")
	releasesPublishOnly := apis.NewBadRequestError("releases are publish-only: use publish/rollback endpoints", nil)
	app.OnRecordCreate("releases").BindFunc(func(e *core.RecordEvent) error {
		if releases.IsInternalSaveCtx(e.Context) {
			return e.Next()
		}
		return releasesPublishOnly
	})
	app.OnRecordDelete("releases").BindFunc(func(e *core.RecordEvent) error {
		if releases.IsInternalSaveCtx(e.Context) {
			return e.Next()
		}
		return releasesPublishOnly
	})
	app.OnRecordUpdate("releases").BindFunc(func(e *core.RecordEvent) error {
		return releasesImmutable
	})
	app.OnRecordValidate("releases").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.IsNew() {
			return e.Next()
		}
		return releasesImmutable
	})

	app.OnRecordCreate("flags").BindFunc(func(e *core.RecordEvent) error {
		if err := checkFlagKey(e.Record.GetString("key")); err != nil {
			return err
		}
		if err := checkFlagKeyTaken(e.App, e.Record); err != nil {
			return err
		}
		n, err := countProjectFlags(e.App, e.Record.GetString("project"))
		if err != nil {
			return err
		}
		if n >= maxFlagsPerProject {
			return errors.New("flag limit reached: max 1000 flags per project")
		}
		return e.Next()
	})
	app.OnRecordUpdate("flags").BindFunc(func(e *core.RecordEvent) error {
		if err := checkFlagKey(e.Record.GetString("key")); err != nil {
			return err
		}
		if err := checkFlagKeyTaken(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordCreate("rules").BindFunc(func(e *core.RecordEvent) error {
		if err := checkRuleRegexConditions(e.Record.Get("condition")); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordUpdate("rules").BindFunc(func(e *core.RecordEvent) error {
		if err := checkRuleRegexConditions(e.Record.Get("condition")); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordDelete("flags").BindFunc(func(e *core.RecordEvent) error {
		// Indexed child lookup (idx_rules_flag) instead of a full-table
		// scan, and one transaction for all child deletes: a mid-loop
		// failure rolls everything back and aborts the parent delete, so
		// a retry converges instead of leaving partial progress. The
		// 0006 cascade handles the remaining child relations.
		records, err := e.App.FindRecordsByFilter("rules", "flag = {:flag}", "", 0, 0, dbx.Params{"flag": e.Record.Id})
		if err != nil {
			return err
		}
		if err := e.App.RunInTransaction(func(txApp core.App) error {
			for _, r := range records {
				if err := txApp.Delete(r); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return e.Next()
	})

	// Environment slugs are unique per project (composite uniqueness on
	// (project, slug): two projects may each own "dev", but one project
	// may not own it twice). Enforced in hooks — not a DB unique index —
	// so every write path is covered (API, dashboard, server-side saves;
	// superusers bypass rules but hooks still fire) and pre-existing
	// duplicate rows never break migration. O(n) scan over environments
	// is fine at ConfigWire scale (tens of rows).
	checkEnvSlug := func(app core.App, rec *core.Record) error {
		recs, err := app.FindAllRecords("environments")
		if err != nil {
			return err
		}
		rows := make([]envresolve.EnvRow, 0, len(recs))
		for _, r := range recs {
			rows = append(rows, envresolve.EnvRow{
				ID:      r.Id,
				Slug:    r.GetString("slug"),
				Project: r.GetString("project"),
			})
		}
		if envresolve.SlugTaken(rows, rec.GetString("project"), rec.GetString("slug"), rec.Id) {
			// ApiError (not a plain error) so the data-API 400 carries
			// the message verbatim instead of "Failed to create record."
			return apis.NewBadRequestError("environment slug already exists for this project", nil)
		}
		return nil
	}
	app.OnRecordCreate("environments").BindFunc(func(e *core.RecordEvent) error {
		envSlugMu.Lock()
		defer envSlugMu.Unlock()
		if err := checkEnvSlug(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordUpdate("environments").BindFunc(func(e *core.RecordEvent) error {
		envSlugMu.Lock()
		defer envSlugMu.Unlock()
		if err := checkEnvSlug(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	// A project's last environment cannot be deleted directly (data
	// API, dashboard, server-side saves): every project must keep at
	// least one environment for publish/fetch to resolve. Project
	// deletion itself still removes everything: cascaded deletes run
	// after the project row is gone, so they pass through.
	app.OnRecordDelete("environments").BindFunc(func(e *core.RecordEvent) error {
		projectID := e.Record.GetString("project")
		if _, err := e.App.FindRecordById("projects", projectID); err != nil {
			return e.Next()
		}
		envDeleteMu.Lock()
		defer envDeleteMu.Unlock()
		n, err := e.App.CountRecords("environments", dbx.HashExp{"project": projectID})
		if err != nil {
			return err
		}
		if n <= 1 {
			return apis.NewBadRequestError("cannot delete the last environment of this project.", nil)
		}
		return e.Next()
	})
}

// printStartBanner logs the server URLs with the ConfigWire admin
// dashboard at / (replacing PocketBase's hidden banner).
// addr is the listener address (se.Server.Addr, i.e. mainAddr).
func printStartBanner(app *pocketbase.PocketBase, addr string) {
	scheme := "http"
	if cmd, _, err := app.RootCmd.Find(os.Args[1:]); err == nil && cmd != nil {
		if v, err := cmd.Flags().GetString("https"); err == nil && v != "" {
			scheme = "https"
		}
	}
	if strings.HasSuffix(addr, ":443") {
		scheme = "https"
	}
	host := addr
	if host == "" {
		host = "127.0.0.1:8090"
	}
	base := scheme + "://" + host
	fmt.Printf("ConfigWire Server started at %s\n", base)
	fmt.Printf("├─ REST API:  %s\n", base+"/api/")
	fmt.Printf("└─ Dashboard: %s\n", base+"/")
}

func main() {
	// HideStartBanner: PocketBase's own banner prints
	// "Dashboard: <base>/_/" (its admin UI), but the ConfigWire
	// admin dashboard is served at / — printStartBanner below
	// prints the corrected URLs instead.
	app := pocketbase.NewWithConfig(pocketbase.Config{
		HideStartBanner: true,
	})

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: true,
	})

	registerConfigwireHooks(app)

	ingest.RegisterKeyCacheHooks(app)

	stream.RegisterHook(app)

	ingest.EnableWAL(app)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Disable PocketBase's default installer: it auto-opens
		// /_/#/pbinstall/<token> in the browser via osutils.LaunchURL
		// on fresh DBs. ConfigWire has its own first-run setup at /
		// (GET/POST /api/v1/admin/setup), so the PocketBase installer
		// affordance is redundant.
		se.InstallerFunc = nil

		if os.Getenv("CONFIGWIRE_CORS_ORIGIN") == "" {
			log.Printf("configwire: CONFIGWIRE_CORS_ORIGIN is unset, fetch CORS defaults to \"*\"; set CONFIGWIRE_CORS_ORIGIN=https://app.example.com in production")
		}
		// Global security headers for every response.
		se.Router.BindFunc(func(re *core.RequestEvent) error {
			security.SetHeaders(re)
			setStaticCacheHeaders(re)
			if !limits.CheckIP(re) {
				if limits.IsAdminDenied(re) {
					return limits.BlockAdmin(re)
				}
				return limits.BlockIP(re)
			}
			return re.Next()
		})

		// Content fingerprint of the Admin UI shell, captured once per
		// process start. The client compares it on a timer to show a
		// "reload to update" banner — no VERSION bump required.
		assets := staticDigest()

		se.Router.GET("/healthz", func(re *core.RequestEvent) error {
			return re.JSON(200, map[string]string{"status": "ok"})
		})

		// Deprecated: GET /hello is deprecated, use GET /healthz instead.
		// It will be removed in the next major release (v0.2.0).
		se.Router.GET("/hello", func(re *core.RequestEvent) error {
			h := re.Response.Header()
			h.Set("Deprecation", "true")
			h.Set("Sunset", "v0.2.0")
			h.Set("Link", `</healthz>; rel="successor-version"`)
			h.Set("Warning", `299 configwire "/hello is deprecated, use /healthz; will be removed in v0.2.0"`)
			return re.String(200, "Hello world!")
		})

		// Public app metadata for the pre-auth login topbar.
		// No auth by design (same as /healthz).
		se.Router.GET("/api/v1/meta", func(re *core.RequestEvent) error {
			return re.JSON(200, map[string]string{"version": appVersion(), "assets": assets})
		})

		limits.Register(se)

		ingest.Register(se)

		ingest.RegisterKeys(se)

		releases.Register(se)

		fetch.Register(se)

		stream.Register(se)

		stats.Register(se)

		purge.Register(se)

		account.Register(se)

		// Static files only; dynamic data via
		// fetch calls from pb_public/js/*.js. Mounted LAST on /{path...}
		// with indexFallback so /api/* (registered above) keeps
		// precedence: unknown /api/* paths still answer 404 JSON, while
		// unknown non-API paths fall back to index.html. Serve runs from
		// configwire/ so ./pb_public resolves (cwd=configwire required).
		se.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), true))

		if err := se.Next(); err != nil {
			return err
		}
		printStartBanner(app, se.Server.Addr)
		return nil
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
