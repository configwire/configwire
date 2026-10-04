package releases

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
)

// promoteHandlerTestApp boots a TestApp the same way
// internal_save_test.go (guardTestApp) and configwire/main_test.go
// (envGuardTestApp) do: tests.NewTestApp + t.Cleanup(app.Cleanup).
// Collections are minimal text/number/bool/json stand-ins for the real
// migration collections — every field the publish/promote path reads is
// modeled (relations stay plain text holding the target id, exactly like
// the handler path reads them via GetString). The releases guards mirror
// configwire/main.go verbatim: create/delete are publish-only (marked
// internalSave passes, bare data-API saves fail), updates always fail as
// immutable.
func promoteHandlerTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	publishOnly := apis.NewBadRequestError("releases are publish-only: use publish/rollback endpoints", nil)
	immutable := errors.New("releases are immutable: publish a new release instead")
	app.OnRecordCreate("releases").BindFunc(func(e *core.RecordEvent) error {
		if IsInternalSaveCtx(e.Context) {
			return e.Next()
		}
		return publishOnly
	})
	app.OnRecordDelete("releases").BindFunc(func(e *core.RecordEvent) error {
		if IsInternalSaveCtx(e.Context) {
			return e.Next()
		}
		return publishOnly
	})
	app.OnRecordUpdate("releases").BindFunc(func(e *core.RecordEvent) error {
		return immutable
	})
	app.OnRecordValidate("releases").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.IsNew() {
			return e.Next()
		}
		return immutable
	})

	collections := map[string][]string{
		"projects": {
			`{"type":"text","name":"name"}`,
		},
		"environments": {
			`{"type":"text","name":"slug"}`,
			`{"type":"text","name":"project"}`,
		},
		"flags": {
			`{"type":"text","name":"project"}`,
			`{"type":"text","name":"key"}`,
			`{"type":"text","name":"type"}`,
			`{"type":"text","name":"group"}`,
			`{"type":"bool","name":"defaultValue"}`,
		},
		"rules": {
			`{"type":"text","name":"flag"}`,
			`{"type":"number","name":"priority"}`,
			`{"type":"json","name":"condition"}`,
			`{"type":"json","name":"value"}`,
		},
		"experiments": {
			`{"type":"text","name":"flag"}`,
			`{"type":"text","name":"seed"}`,
			`{"type":"json","name":"variants"}`,
			`{"type":"text","name":"status"}`,
		},
		"releases": {
			`{"type":"text","name":"env"}`,
			`{"type":"number","name":"version"}`,
			`{"type":"text","name":"etag"}`,
			`{"type":"json","name":"snapshot"}`,
			`{"type":"text","name":"author"}`,
			`{"type":"text","name":"note"}`,
		},
	}
	for name, fields := range collections {
		col := core.NewBaseCollection(name)
		for _, f := range fields {
			if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
				t.Fatalf("add %s field %s: %v", name, f, err)
			}
		}
		if err := app.Save(col); err != nil {
			t.Fatalf("save %s collection: %v", name, err)
		}
	}
	return app
}

func seedPromoteProject(t *testing.T, app *tests.TestApp, name string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		t.Fatalf("find projects: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save project %s: %v", name, err)
	}
	return rec.Id
}

func seedPromoteEnv(t *testing.T, app *tests.TestApp, projectID, slug string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", projectID)
	rec.Set("slug", slug)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env %s: %v", slug, err)
	}
	return rec.Id
}

// seedPromoteFlag stores one bool flag (default false, no rules) so
// BuildSnapshot assembles a non-empty, valid snapshot for the project.
func seedPromoteFlag(t *testing.T, app *tests.TestApp, projectID, key string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", projectID)
	rec.Set("key", key)
	rec.Set("type", "bool")
	rec.Set("group", "")
	rec.Set("defaultValue", false)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save flag %s: %v", key, err)
	}
}

// callPublish invokes postPublish directly (the real publish path,
// including server-side BuildSnapshot) and reports the HTTP status plus
// the decoded JSON body for 2xx or the ApiError message for non-2xx.
func callPublish(t *testing.T, app *tests.TestApp, slug, projectQuery, body string) (int, map[string]any, string) {
	t.Helper()
	url := "/api/v1/admin/env/" + slug + "/publish"
	if projectQuery != "" {
		url += "?project=" + projectQuery
	}
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("env", slug)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postPublish(re); err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status, nil, apiErr.Message
		}
		t.Fatalf("postPublish(%s): non ApiError %v", slug, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode postPublish(%s) body: %v", slug, err)
	}
	return rec.Code, decoded, ""
}

// callPromote invokes postPromoteEnv directly and reports the HTTP status
// plus the decoded JSON body for 2xx (including the 409 stale-base JSON,
// which the handler writes via re.JSON) or the ApiError message for
// 400/404 paths. An empty body sends a nil reader (ContentLength 0), the
// same shape as an empty POST.
func callPromote(t *testing.T, app *tests.TestApp, dest, projectQuery, body string) (int, map[string]any, string) {
	t.Helper()
	url := "/api/v1/admin/env/" + dest + "/promote"
	if projectQuery != "" {
		url += "?project=" + projectQuery
	}
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, url, nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("dest", dest)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postPromoteEnv(re); err != nil {
		var apiErr *router.ApiError
		if errors.As(err, &apiErr) {
			return apiErr.Status, nil, apiErr.Message
		}
		t.Fatalf("postPromoteEnv(%s): non ApiError %v", dest, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode postPromoteEnv(%s) body %q: %v", dest, rec.Body.String(), err)
	}
	return rec.Code, decoded, ""
}

func promoteMax(t *testing.T, app *tests.TestApp, envID string) int {
	t.Helper()
	max, err := MaxVersionForEnv(app, envID)
	if err != nil {
		t.Fatalf("MaxVersionForEnv: %v", err)
	}
	return max
}

func countPromoteReleases(t *testing.T, app *tests.TestApp, envID string) int {
	t.Helper()
	recs, err := app.FindAllRecords("releases")
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	n := 0
	for _, r := range recs {
		if r.GetString("env") == envID {
			n++
		}
	}
	return n
}

func findPromoteRelease(t *testing.T, app *tests.TestApp, envID string, version int) *core.Record {
	t.Helper()
	recs, err := app.FindAllRecords("releases")
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	for _, r := range recs {
		if r.GetString("env") == envID && r.GetInt("version") == version {
			return r
		}
	}
	t.Fatalf("no release env=%s version=%d", envID, version)
	return nil
}

func snapshotBytesOf(t *testing.T, rec *core.Record) string {
	t.Helper()
	b, err := rawSnapshotBytes(rec.GetRaw("snapshot"))
	if err != nil {
		t.Fatalf("rawSnapshotBytes: %v", err)
	}
	return string(b)
}

// TestPostPromoteDevV2ToProdBase0 is the happy-path matrix cell: dev@v2
// promoted into an empty prod lands as prod v1 with byte-identical
// snapshot bytes and a fresh etag (version is part of the etag input, so
// identical bytes still hash differently).
func TestPostPromoteDevV2ToProdBase0(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	dev := seedPromoteEnv(t, app, p1, "dev")
	prod := seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")

	if code, body, msg := callPublish(t, app, "dev", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK || body["version"] != float64(1) {
		t.Fatalf("publish dev v1: code=%d body=%v msg=%q, want 200 version 1", code, body, msg)
	}
	if code, body, msg := callPublish(t, app, "dev", "", `{"note":"v2","baseVersion":1}`); code != http.StatusOK || body["version"] != float64(2) {
		t.Fatalf("publish dev v2: code=%d body=%v msg=%q, want 200 version 2", code, body, msg)
	}

	src := findPromoteRelease(t, app, dev, 2)
	srcSnap := snapshotBytesOf(t, src)
	srcEtag := src.GetString("etag")
	if srcEtag == "" {
		t.Fatal("dev v2 etag is empty")
	}

	code, body, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":2,"destBaseVersion":0,"note":"promote v2"}`)
	if code != http.StatusOK {
		t.Fatalf("promote dev@v2->prod: code=%d msg=%q, want 200", code, msg)
	}
	if body["version"] != float64(1) {
		t.Fatalf("promote body version = %v, want 1", body["version"])
	}
	destEtag, _ := body["etag"].(string)
	if destEtag == "" {
		t.Fatalf("promote body etag missing: %v", body)
	}

	dest := findPromoteRelease(t, app, prod, 1)
	if got := snapshotBytesOf(t, dest); got != srcSnap {
		t.Fatalf("prod v1 snapshot differs from dev v2:\n got %s\nwant %s", got, srcSnap)
	}
	if dest.GetString("etag") == srcEtag {
		t.Fatal("prod v1 etag equals dev v2 etag, want a fresh etag")
	}
	if dest.GetString("etag") != destEtag {
		t.Fatalf("stored etag %q != response etag %q", dest.GetString("etag"), destEtag)
	}
	if want := EtagFor(1, []byte(srcSnap)); dest.GetString("etag") != want {
		t.Fatalf("prod v1 etag = %q, want EtagFor(1, srcBytes) = %q", dest.GetString("etag"), want)
	}
	if got := promoteMax(t, app, prod); got != 1 {
		t.Fatalf("prod max = %d, want 1", got)
	}
}

// TestPostPromoteStaleDestBase409NoWrite pins the 409 cell: a stale
// destBaseVersion answers 409 with the live currentVersion and writes
// nothing (max before == max after, row count unchanged).
func TestPostPromoteStaleDestBase409NoWrite(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	dev := seedPromoteEnv(t, app, p1, "dev")
	prod := seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")

	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish dev v1: code=%d msg=%q, want 200", code, msg)
	}
	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"v2","baseVersion":1}`); code != http.StatusOK {
		t.Fatalf("publish dev v2: code=%d msg=%q, want 200", code, msg)
	}
	if code, _, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":2,"destBaseVersion":0,"note":"first"}`); code != http.StatusOK {
		t.Fatalf("first promote: code=%d msg=%q, want 200", code, msg)
	}

	before := promoteMax(t, app, prod)
	if before != 1 {
		t.Fatalf("prod max before stale call = %d, want 1", before)
	}
	rowsBefore := countPromoteReleases(t, app, prod)

	code, body, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":2,"destBaseVersion":0,"note":"stale"}`)
	if code != http.StatusConflict {
		t.Fatalf("stale promote: code=%d body=%v msg=%q, want 409", code, body, msg)
	}
	if body["currentVersion"] != float64(before) {
		t.Fatalf("409 body currentVersion = %v, want %d", body["currentVersion"], before)
	}
	_ = dev

	if after := promoteMax(t, app, prod); after != before {
		t.Fatalf("prod max after stale call = %d, want %d (no write)", after, before)
	}
	if got := countPromoteReleases(t, app, prod); got != rowsBefore {
		t.Fatalf("prod rows after stale call = %d, want %d (no write)", got, rowsBefore)
	}
}

// TestPostPromoteUnknownSrcVersion404 pins the 404 cell: a srcVersion
// with no row in the src env answers 404 and writes nothing to dest.
func TestPostPromoteUnknownSrcVersion404(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	dev := seedPromoteEnv(t, app, p1, "dev")
	prod := seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")

	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish dev v1: code=%d msg=%q, want 200", code, msg)
	}

	before := promoteMax(t, app, prod)
	code, _, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":99,"destBaseVersion":0,"note":"ghost"}`)
	if code != http.StatusNotFound {
		t.Fatalf("unknown src version: code=%d msg=%q, want 404", code, msg)
	}
	if !strings.Contains(msg, "Unknown release version.") {
		t.Fatalf("404 message = %q, want it to mention the unknown release version", msg)
	}
	_ = dev
	if after := promoteMax(t, app, prod); after != before {
		t.Fatalf("prod max after 404 = %d, want %d (no write)", after, before)
	}
}

// TestPostPromoteCrossProject400 pins the cross-project cell: src and
// dest envs in different projects answer 400 naming the rule, with no
// write to dest.
func TestPostPromoteCrossProject400(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	dev := seedPromoteEnv(t, app, p1, "dev")
	prod := seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")
	p2 := seedPromoteProject(t, app, "p2")
	seedPromoteEnv(t, app, p2, "staging")
	seedPromoteFlag(t, app, p2, "launch_flag")

	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish dev v1: code=%d msg=%q, want 200", code, msg)
	}
	if code, _, msg := callPublish(t, app, "staging", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish staging v1: code=%d msg=%q, want 200", code, msg)
	}

	before := promoteMax(t, app, prod)
	code, _, msg := callPromote(t, app, "prod", "", `{"srcEnv":"staging","srcVersion":1,"destBaseVersion":0,"note":"x-project"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("cross-project promote: code=%d msg=%q, want 400", code, msg)
	}
	if !strings.Contains(msg, "across projects") {
		t.Fatalf("400 message = %q, want it to mention promotion across projects", msg)
	}
	_ = dev
	if after := promoteMax(t, app, prod); after != before {
		t.Fatalf("prod max after 400 = %d, want %d (no write)", after, before)
	}
}

// TestPostPromoteAmbiguousSrcSlug400ThenQualified200 pins the ambiguity
// cell: two projects may each own a "dev" slug (uniqueness is per
// (project, slug)), so a bare srcEnv answers 400 naming the ambiguous
// slug; qualifying with the owning srcProject promotes cleanly.
func TestPostPromoteAmbiguousSrcSlug400ThenQualified200(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	dev1 := seedPromoteEnv(t, app, p1, "dev")
	prod := seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")
	p2 := seedPromoteProject(t, app, "p2")
	seedPromoteEnv(t, app, p2, "dev")
	seedPromoteFlag(t, app, p2, "other_flag")

	if code, _, msg := callPublish(t, app, "dev", p1, `{"note":"p1 v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish p1 dev v1: code=%d msg=%q, want 200", code, msg)
	}
	if code, _, msg := callPublish(t, app, "dev", p2, `{"note":"p2 v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish p2 dev v1: code=%d msg=%q, want 200", code, msg)
	}

	code, _, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":1,"destBaseVersion":0,"note":"bare"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("bare shared srcEnv: code=%d msg=%q, want 400", code, msg)
	}
	if !strings.Contains(strings.ToLower(msg), "ambiguous env slug") {
		t.Fatalf("400 message = %q, want it to mention the ambiguous env slug", msg)
	}
	if got := promoteMax(t, app, prod); got != 0 {
		t.Fatalf("prod max after ambiguous 400 = %d, want 0 (no write)", got)
	}

	code, body, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcProject":"`+p1+`","srcVersion":1,"destBaseVersion":0,"note":"qualified"}`)
	if code != http.StatusOK {
		t.Fatalf("qualified promote: code=%d msg=%q, want 200", code, msg)
	}
	if body["version"] != float64(1) {
		t.Fatalf("qualified promote body version = %v, want 1", body["version"])
	}
	src := findPromoteRelease(t, app, dev1, 1)
	dest := findPromoteRelease(t, app, prod, 1)
	if snapshotBytesOf(t, dest) != snapshotBytesOf(t, src) {
		t.Fatal("qualified promote: prod v1 snapshot differs from p1 dev v1 bytes")
	}
	// No etag-freshness assertion here: src and dest are both version 1
	// over identical bytes, so EtagFor(1, bytes) is equal by
	// construction (freshness across versions is pinned in
	// TestPostPromoteDevV2ToProdBase0).
}

// TestPromoteHandlerDirectWritesDenied proves the main.go hooks stayed
// intact: a bare data-API create is rejected publish-only and a patch of
// an existing row is rejected immutable, with the row count unchanged.
func TestPromoteHandlerDirectWritesDenied(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	dev := seedPromoteEnv(t, app, p1, "dev")
	seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")

	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish dev v1: code=%d msg=%q, want 200", code, msg)
	}
	rowsBefore := countPromoteReleases(t, app, dev)

	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	direct := core.NewRecord(col)
	direct.Set("env", dev)
	direct.Set("version", 99)
	direct.Set("etag", "direct-etag")
	direct.Set("snapshot", `{"flags":[],"experiments":[]}`)
	direct.Set("author", "direct")
	direct.Set("note", "direct")
	if err := app.Save(direct); err == nil || !strings.Contains(err.Error(), "publish-only") {
		t.Fatalf("direct create: err=%v, want publish-only rejection", err)
	}
	if err := app.SaveWithContext(context.Background(), direct); err == nil || !strings.Contains(err.Error(), "publish-only") {
		t.Fatalf("direct create (background ctx): err=%v, want publish-only rejection", err)
	}

	existing := findPromoteRelease(t, app, dev, 1)
	existing.Set("note", "patched")
	if err := app.Save(existing); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("direct patch: err=%v, want immutable rejection", err)
	}
	if got := countPromoteReleases(t, app, dev); got != rowsBefore {
		t.Fatalf("dev rows after denied writes = %d, want %d (no write)", got, rowsBefore)
	}
	fresh := findPromoteRelease(t, app, dev, 1)
	if fresh.GetString("note") == "patched" {
		t.Fatal("dev v1 note was patched, want the immutable row unchanged")
	}
}

// TestPostPromoteStrictBodyDecoding pins decodeBody strictness: a
// destBaseVersion sent as a JSON string fails decoding (400), and an
// empty body is 400 — both with no write to dest.
func TestPostPromoteStrictBodyDecoding(t *testing.T) {
	app := promoteHandlerTestApp(t)
	p1 := seedPromoteProject(t, app, "p1")
	seedPromoteEnv(t, app, p1, "dev")
	prod := seedPromoteEnv(t, app, p1, "prod")
	seedPromoteFlag(t, app, p1, "launch_flag")

	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"v1","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish dev v1: code=%d msg=%q, want 200", code, msg)
	}

	code, _, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":1,"destBaseVersion":"0","note":"string-base"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("string destBaseVersion: code=%d msg=%q, want 400", code, msg)
	}
	code, _, msg = callPromote(t, app, "prod", "", "")
	if code != http.StatusBadRequest {
		t.Fatalf("empty body: code=%d msg=%q, want 400", code, msg)
	}
	if !strings.Contains(strings.ToLower(msg), "empty body") {
		t.Fatalf("empty-body message = %q, want it to mention the empty body", msg)
	}
	if got := promoteMax(t, app, prod); got != 0 {
		t.Fatalf("prod max after decoder 400s = %d, want 0 (no write)", got)
	}
}
