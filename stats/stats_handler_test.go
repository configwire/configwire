package stats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// flagIgnoranceApp builds a TestApp with the four collections getStats
// reads: environments, events, event_daily, releases. Relation targets
// are plain text holding the env id: the loaders only read them via
// GetString, exactly like the handler path. event_daily stays empty so
// the 7d window is events-only (approximate:false) and timing-flake-proof.
//
// Seeded rows carry distinct flags so per-flag filtering is provable:
// exp_bool gets 2 fetches (v1) + exposures control:1 treatment:1;
// other_flag gets 1 fetch (v1) + 1 exposure (treatment). Env-wide that
// is 3 fetches + 3 exposures = 6 total.
func flagIgnoranceApp(t *testing.T) (*tests.TestApp, string) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	envs := core.NewBaseCollection("environments")
	for _, f := range []string{
		`{"type":"text","name":"slug"}`,
		`{"type":"text","name":"project"}`,
	} {
		if err := envs.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add environments field %s: %v", f, err)
		}
	}
	if err := app.Save(envs); err != nil {
		t.Fatalf("save environments collection: %v", err)
	}

	events := core.NewBaseCollection("events")
	for _, f := range []string{
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"date","name":"ts"}`,
	} {
		if err := events.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add events field %s: %v", f, err)
		}
	}
	if err := app.Save(events); err != nil {
		t.Fatalf("save events collection: %v", err)
	}

	daily := core.NewBaseCollection("event_daily")
	for _, f := range []string{
		`{"type":"date","name":"day"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"number","name":"fetches"}`,
		`{"type":"number","name":"exposures"}`,
	} {
		if err := daily.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add event_daily field %s: %v", f, err)
		}
	}
	if err := app.Save(daily); err != nil {
		t.Fatalf("save event_daily collection: %v", err)
	}

	releases := core.NewBaseCollection("releases")
	for _, f := range []string{
		`{"type":"text","name":"env"}`,
		`{"type":"number","name":"version"}`,
	} {
		if err := releases.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add releases field %s: %v", f, err)
		}
	}
	if err := app.Save(releases); err != nil {
		t.Fatalf("save releases collection: %v", err)
	}

	// One unambiguous env row: no ?project= needed.
	envCol, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	envRec := core.NewRecord(envCol)
	envRec.Set("slug", "dev")
	envRec.Set("project", "p1")
	if err := app.Save(envRec); err != nil {
		t.Fatalf("save env: %v", err)
	}
	envID := envRec.Id

	// Fresh rows far from the 7d cutoff, scoped to distinct flags.
	// No userHash field: loadRows never reads it.
	now := time.Now().UTC()
	eventsCol, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	seed := func(flag, kind, variant string, version int) {
		t.Helper()
		rec := core.NewRecord(eventsCol)
		rec.Set("env", envID)
		rec.Set("flag", flag)
		rec.Set("kind", kind)
		rec.Set("variant", variant)
		rec.Set("version", version)
		rec.Set("ts", now)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save event: %v", err)
		}
	}
	seed("exp_bool", "fetch", "", 1)
	seed("exp_bool", "fetch", "", 1)
	seed("exp_bool", "exposure", "control", 1)
	seed("exp_bool", "exposure", "treatment", 1)
	seed("other_flag", "fetch", "", 1)
	seed("other_flag", "exposure", "treatment", 1)

	relCol, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	relRec := core.NewRecord(relCol)
	relRec.Set("env", envID)
	relRec.Set("version", 1)
	if err := app.Save(relRec); err != nil {
		t.Fatalf("save release: %v", err)
	}

	return app, envID
}

// callStats invokes the unexported getStats handler directly with the
// given raw query string and returns the status code plus decoded body.
func callStats(t *testing.T, app *tests.TestApp, rawQuery string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/env/dev/stats"+rawQuery, nil)
	req.SetPathValue("env", "dev")
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getStats(re); err != nil {
		t.Fatalf("getStats(%q): %v", rawQuery, err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode getStats(%q) body: %v", rawQuery, err)
	}
	return rec.Code, body
}

// TestGetStatsFiltersByFlag proves GET stats ?flag= scoping against the
// (day,env,flag,variant,version) code: ?flag=exp_bool returns that flag's
// counts only with flagFound:true and echo.flag echoed; ?flag=unknown
// returns zeros with flagFound:false and echo.flag echoed; no-param
// returns env-wide totals with no flagFound key and echo without flag;
// an over-long flag maps to 400 (BadRequestError from ParseFlag).
//
// This test does not assert the 401 superuser path: it calls getStats
// directly, skipping the RequireSuperuserAuth middleware by design;
// counting/flag-filtering is orthogonal to auth.
func TestGetStatsFiltersByFlag(t *testing.T) {
	app, _ := flagIgnoranceApp(t)

	num := func(body map[string]any, key string) float64 {
		t.Helper()
		v, ok := body[key].(float64)
		if !ok {
			t.Fatalf("%v is not a number: %v", key, body[key])
		}
		return v
	}
	echoOf := func(body map[string]any, query string) map[string]any {
		t.Helper()
		echo, ok := body["echo"].(map[string]any)
		if !ok {
			t.Fatalf("getStats(%q) echo is not an object: %v", query, body["echo"])
		}
		return echo
	}

	// No-param: env-wide totals (3 fetches + 3 exposures = 6), no
	// flagFound key, echo without flag.
	codePlain, plain := callStats(t, app, "?since=7d")
	if codePlain != http.StatusOK {
		t.Fatalf("no-param status = %d, want 200", codePlain)
	}
	if got := num(plain, "fetches"); got != 3 {
		t.Errorf("no-param fetches = %v, want 3", got)
	}
	if got := num(plain, "exposures"); got != 3 {
		t.Errorf("no-param exposures = %v, want 3", got)
	}
	if got := num(plain, "total"); got != 6 {
		t.Errorf("no-param total = %v, want 6", got)
	}
	if _, ok := plain["flagFound"]; ok {
		t.Errorf("no-param carries flagFound key: %v", plain["flagFound"])
	}
	if _, ok := echoOf(plain, "no-param")["flag"]; ok {
		t.Errorf("no-param echo carries flag key: %v", plain["echo"])
	}
	if plain["approximate"] != false {
		t.Errorf("no-param approximate = %v, want false (events-only window)", plain["approximate"])
	}

	// ?flag=exp_bool: that flag's counts only + flagFound:true + echo.flag.
	codeExp, exp := callStats(t, app, "?since=7d&flag=exp_bool")
	if codeExp != http.StatusOK {
		t.Fatalf("?flag=exp_bool status = %d, want 200", codeExp)
	}
	if got := num(exp, "fetches"); got != 2 {
		t.Errorf("exp_bool fetches = %v, want 2", got)
	}
	if got := num(exp, "exposures"); got != 2 {
		t.Errorf("exp_bool exposures = %v, want 2", got)
	}
	if got := num(exp, "total"); got != 4 {
		t.Errorf("exp_bool total = %v, want 4", got)
	}
	perVariant, ok := exp["perVariant"].(map[string]any)
	if !ok {
		t.Fatalf("exp_bool perVariant is not an object: %v", exp["perVariant"])
	}
	if perVariant["control"] != float64(1) || perVariant["treatment"] != float64(1) {
		t.Errorf("exp_bool perVariant = %v, want control:1 treatment:1", perVariant)
	}
	if got := num(exp, "version"); got != 1 {
		t.Errorf("exp_bool version = %v, want 1", got)
	}
	if exp["flagFound"] != true {
		t.Errorf("exp_bool flagFound = %v, want true", exp["flagFound"])
	}
	if got := echoOf(exp, "?flag=exp_bool")["flag"]; got != "exp_bool" {
		t.Errorf("exp_bool echo.flag = %v, want exp_bool", got)
	}

	// ?flag=other_flag: the other flag's counts only.
	codeOther, other := callStats(t, app, "?since=7d&flag=other_flag")
	if codeOther != http.StatusOK {
		t.Fatalf("?flag=other_flag status = %d, want 200", codeOther)
	}
	if got := num(other, "fetches"); got != 1 {
		t.Errorf("other_flag fetches = %v, want 1", got)
	}
	if got := num(other, "exposures"); got != 1 {
		t.Errorf("other_flag exposures = %v, want 1", got)
	}
	if got := num(other, "total"); got != 2 {
		t.Errorf("other_flag total = %v, want 2", got)
	}
	if other["flagFound"] != true {
		t.Errorf("other_flag flagFound = %v, want true", other["flagFound"])
	}
	if got := echoOf(other, "?flag=other_flag")["flag"]; got != "other_flag" {
		t.Errorf("other_flag echo.flag = %v, want other_flag", got)
	}

	// ?flag=unknown: matches nothing, so zeros + flagFound:false +
	// echo.flag echoed back.
	codeUnknown, unknown := callStats(t, app, "?since=7d&flag=unknown_flag_xyz")
	if codeUnknown != http.StatusOK {
		t.Fatalf("?flag=unknown status = %d, want 200", codeUnknown)
	}
	if got := num(unknown, "fetches"); got != 0 {
		t.Errorf("unknown fetches = %v, want 0", got)
	}
	if got := num(unknown, "exposures"); got != 0 {
		t.Errorf("unknown exposures = %v, want 0", got)
	}
	if got := num(unknown, "total"); got != 0 {
		t.Errorf("unknown total = %v, want 0", got)
	}
	if unknown["flagFound"] != false {
		t.Errorf("unknown flagFound = %v, want false", unknown["flagFound"])
	}
	if got := echoOf(unknown, "?flag=unknown")["flag"]; got != "unknown_flag_xyz" {
		t.Errorf("unknown echo.flag = %v, want unknown_flag_xyz", got)
	}

	// Over-long flag: ParseFlag rejects >64 chars, which the handler
	// maps to 400 (BadRequestError). callStats would Fatalf on the
	// returned error, so invoke getStats directly and assert the error.
	longFlag := strings.Repeat("f", MaxFlagLen+1)
	if _, err := ParseFlag(longFlag); err == nil {
		t.Fatalf("ParseFlag(65 chars): expected error, got nil")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/env/dev/stats?since=7d&flag="+longFlag, nil)
	req.SetPathValue("env", "dev")
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := getStats(re); err == nil {
		t.Errorf("getStats over-long flag: expected 400 error, got nil")
	} else if !strings.Contains(err.Error(), "at most 64") {
		t.Errorf("getStats over-long flag error = %q, want flag length cap", err.Error())
	}
}
