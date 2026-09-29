package stats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	// Fresh rows far from the 7d cutoff: 2 fetches + 2 exposures.
	// No userHash field: loadRows never reads it.
	now := time.Now().UTC()
	eventsCol, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	seed := func(kind, variant string, version int) {
		t.Helper()
		rec := core.NewRecord(eventsCol)
		rec.Set("env", envID)
		rec.Set("kind", kind)
		rec.Set("variant", variant)
		rec.Set("version", version)
		rec.Set("ts", now)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save event: %v", err)
		}
	}
	seed("fetch", "", 1)
	seed("fetch", "", 1)
	seed("exposure", "control", 1)
	seed("exposure", "treatment", 1)

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

// assertNoFlagKeys fails if "flag" or "flagFound" appears as a key
// anywhere in the decoded response tree, including inside echo.
func assertNoFlagKeys(t *testing.T, rawQuery string, v any) {
	t.Helper()
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "flag" || k == "flagFound" {
				t.Fatalf("getStats(%q): response carries %q key", rawQuery, k)
			}
			assertNoFlagKeys(t, rawQuery, child)
		}
	case []any:
		for _, child := range v {
			assertNoFlagKeys(t, rawQuery, child)
		}
	}
}

// TestGetStatsIgnoresFlagParam proves GET stats with ?flag=anything
// returns 200 with env-wide counts identical to the no-param request:
// getStats reads only PathValue("env") plus ?project= and ?since=, so a
// stale ?flag= is ignored by construction. This pins that posture now
// that the e2e unknown-flag block is gone.
//
// This test does not assert the 401 superuser path: it calls getStats
// directly, skipping the RequireSuperuserAuth middleware by design;
// counting/param-ignorance is orthogonal to auth.
func TestGetStatsIgnoresFlagParam(t *testing.T) {
	app, _ := flagIgnoranceApp(t)

	codePlain, plain := callStats(t, app, "?since=7d")
	if codePlain != http.StatusOK {
		t.Fatalf("no-param status = %d, want 200", codePlain)
	}
	codeFlag, flagged := callStats(t, app, "?since=7d&flag=anything")
	if codeFlag != http.StatusOK {
		t.Fatalf("?flag=anything status = %d, want 200", codeFlag)
	}
	codePath, pathish := callStats(t, app, "?since=7d&flag=../x")
	if codePath != http.StatusOK {
		t.Fatalf("?flag=../x status = %d, want 200", codePath)
	}

	// Identical bodies, field by field. echo carries the live cutoff
	// timestamp, so it is compared with time-tolerance instead of
	// exact DeepEqual; everything else must match exactly.
	for _, key := range []string{
		"fetches", "exposures", "perVariant", "perVersion",
		"total", "rates", "sources", "approximate", "series", "version",
	} {
		if !reflect.DeepEqual(flagged[key], plain[key]) {
			t.Errorf("?flag=anything changed %q: got %v, want %v (no-param)", key, flagged[key], plain[key])
		}
		if !reflect.DeepEqual(pathish[key], plain[key]) {
			t.Errorf("?flag=../x changed %q: got %v, want %v (no-param)", key, pathish[key], plain[key])
		}
	}
	for name, body := range map[string]map[string]any{"flagged": flagged, "pathish": pathish} {
		plainEcho, ok := plain["echo"].(map[string]any)
		if !ok {
			t.Fatalf("no-param echo is not an object: %v", plain["echo"])
		}
		echo, ok := body["echo"].(map[string]any)
		if !ok {
			t.Fatalf("%s echo is not an object: %v", name, body["echo"])
		}
		for _, key := range []string{"env", "since", "sinceDays", "horizon"} {
			if !reflect.DeepEqual(echo[key], plainEcho[key]) {
				t.Errorf("%s echo[%q] = %v, want %v (no-param)", name, key, echo[key], plainEcho[key])
			}
		}
		for _, key := range []string{"cutoff", "rollupHorizon"} {
			got, err := time.Parse(time.RFC3339, echo[key].(string))
			if err != nil {
				t.Fatalf("%s echo[%q] %v does not parse as RFC3339: %v", name, key, echo[key], err)
			}
			want, err := time.Parse(time.RFC3339, plainEcho[key].(string))
			if err != nil {
				t.Fatalf("no-param echo[%q] %v does not parse as RFC3339: %v", key, plainEcho[key], err)
			}
			if got.Sub(want).Abs() > 5*time.Second {
				t.Errorf("%s echo[%q] = %v, want ~%v", name, key, got, want)
			}
		}
	}

	// No flag-shaped keys anywhere, including inside echo.
	for name, body := range map[string]map[string]any{
		"no-param": plain, "flagged": flagged, "pathish": pathish,
	} {
		assertNoFlagKeys(t, name, body)
	}

	// Positive env-wide values equal to the seeded expectation,
	// not just equal-zeros: 2 fetches + 2 exposures = 4 total.
	num := func(body map[string]any, key string) float64 {
		t.Helper()
		v, ok := body[key].(float64)
		if !ok {
			t.Fatalf("%v is not a number: %v", key, body[key])
		}
		return v
	}
	if got := num(flagged, "fetches"); got != 2 {
		t.Errorf("flagged fetches = %v, want 2", got)
	}
	if got := num(flagged, "exposures"); got != 2 {
		t.Errorf("flagged exposures = %v, want 2", got)
	}
	if got := num(flagged, "total"); got != 4 {
		t.Errorf("flagged total = %v, want 4", got)
	}
	perVariant, ok := flagged["perVariant"].(map[string]any)
	if !ok {
		t.Fatalf("flagged perVariant is not an object: %v", flagged["perVariant"])
	}
	if perVariant["control"] != float64(1) || perVariant["treatment"] != float64(1) {
		t.Errorf("flagged perVariant = %v, want control:1 treatment:1", perVariant)
	}
	if got := num(flagged, "version"); got != 1 {
		t.Errorf("flagged version = %v, want 1", got)
	}
	if flagged["approximate"] != false {
		t.Errorf("flagged approximate = %v, want false (events-only window)", flagged["approximate"])
	}
}
