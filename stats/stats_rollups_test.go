// Stats read-path tests for the completed-day rollup split: auto mode
// reads today from raw events and past days from event_daily, the legacy
// kill switch reproduces the historic raw-window numbers, and an
// incomplete backfill falls back safely.
package stats

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"

	"github.com/configwire/configwire/purge"
)

type rollupStatsApp struct {
	app     *tests.TestApp
	envID   string
	flagIDs map[string]string
}

// newRollupStatsApp builds the minimal collections getStats reads plus the
// rollup_state singleton used by the readiness gate.
func newRollupStatsApp(t *testing.T) *rollupStatsApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	mkCol := func(name string, fields ...string) *core.Collection {
		t.Helper()
		col := core.NewBaseCollection(name)
		for _, f := range fields {
			if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
				t.Fatalf("add field %s to %s: %v", f, name, err)
			}
		}
		if err := app.Save(col); err != nil {
			t.Fatalf("save collection %s: %v", name, err)
		}
		return col
	}
	mkCol("environments",
		`{"type":"text","name":"slug"}`,
		`{"type":"text","name":"project"}`,
	)
	mkCol("flags",
		`{"type":"text","name":"project"}`,
		`{"type":"text","name":"key"}`,
	)
	mkCol("events",
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"date","name":"ts"}`,
	)
	mkCol("event_daily",
		`{"type":"date","name":"day"}`,
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"number","name":"version"}`,
		`{"type":"number","name":"fetches"}`,
		`{"type":"number","name":"exposures"}`,
	)
	mkCol("releases",
		`{"type":"text","name":"env"}`,
		`{"type":"number","name":"version"}`,
	)
	mkCol("rollup_state",
		`{"type":"text","name":"key","required":true}`,
		`{"type":"date","name":"lastDay"}`,
	)

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

	flagCol, err := app.FindCollectionByNameOrId("flags")
	if err != nil {
		t.Fatalf("find flags: %v", err)
	}
	flagIDs := map[string]string{}
	for _, key := range []string{"exp_bool", "other_flag"} {
		rec := core.NewRecord(flagCol)
		rec.Set("project", "p1")
		rec.Set("key", key)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save flag %s: %v", key, err)
		}
		flagIDs[key] = rec.Id
	}

	return &rollupStatsApp{app: app, envID: envRec.Id, flagIDs: flagIDs}
}

func (f *rollupStatsApp) seedRaw(t *testing.T, flagKey, kind, variant string, version int, ts time.Time) {
	t.Helper()
	col, err := f.app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", f.envID)
	if flagKey != "" {
		rec.Set("flag", f.flagIDs[flagKey])
	}
	rec.Set("kind", kind)
	rec.Set("variant", variant)
	rec.Set("version", version)
	rec.Set("ts", ts)
	if err := f.app.Save(rec); err != nil {
		t.Fatalf("save event: %v", err)
	}
}

func (f *rollupStatsApp) seedRollup(t *testing.T, day time.Time, flagKey, variant string, version, fetches, exposures int) {
	t.Helper()
	col, err := f.app.FindCollectionByNameOrId("event_daily")
	if err != nil {
		t.Fatalf("find event_daily: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("day", purge.DayBucket(day))
	rec.Set("env", f.envID)
	if flagKey != "" {
		rec.Set("flag", f.flagIDs[flagKey])
	}
	rec.Set("variant", variant)
	rec.Set("version", version)
	rec.Set("fetches", fetches)
	rec.Set("exposures", exposures)
	if err := f.app.Save(rec); err != nil {
		t.Fatalf("save rollup: %v", err)
	}
}

func (f *rollupStatsApp) markReady(t *testing.T, now time.Time) {
	t.Helper()
	col, err := f.app.FindCollectionByNameOrId("rollup_state")
	if err != nil {
		t.Fatalf("find rollup_state: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", "global")
	rec.Set("lastDay", purge.DayBucket(now).AddDate(0, 0, -1))
	if err := f.app.Save(rec); err != nil {
		t.Fatalf("save rollup_state: %v", err)
	}
}

func numField(t *testing.T, body map[string]any, key string) float64 {
	t.Helper()
	v, ok := body[key].(float64)
	if !ok {
		t.Fatalf("%v is not a number: %v", key, body[key])
	}
	return v
}

func sourcesOf(t *testing.T, body map[string]any) (events, rollups float64) {
	t.Helper()
	src, ok := body["sources"].(map[string]any)
	if !ok {
		t.Fatalf("sources is not an object: %v", body["sources"])
	}
	return numField(t, src, "events"), numField(t, src, "rollups")
}

func echoOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	echo, ok := body["echo"].(map[string]any)
	if !ok {
		t.Fatalf("echo is not an object: %v", body["echo"])
	}
	return echo
}

func seriesSums(t *testing.T, body map[string]any) (fetches, exposures int) {
	t.Helper()
	points, ok := body["series"].([]any)
	if !ok {
		t.Fatalf("series is not an array: %v", body["series"])
	}
	for _, raw := range points {
		p, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("series point is not an object: %v", raw)
		}
		fetches += int(numField(t, p, "fetches"))
		exposures += int(numField(t, p, "exposures"))
	}
	return fetches, exposures
}

// TestStatsAutoReadsTodayRawAndCompletedDayRollups pins the auto split:
// today's rows come from events, completed days from event_daily, a raw
// row already past the horizon is ignored (never counted twice), and the
// wire flags + echo reflect the split actually used.
func TestStatsAutoReadsTodayRawAndCompletedDayRollups(t *testing.T) {
	f := newRollupStatsApp(t)
	now := time.Now().UTC()
	today := purge.DayBucket(now)
	yesterday := today.AddDate(0, 0, -1)
	f.markReady(t, now)

	f.seedRaw(t, "exp_bool", "fetch", "", 2, today)
	f.seedRaw(t, "exp_bool", "exposure", "control", 0, today.Add(23*time.Hour+59*time.Minute+59*time.Second))
	f.seedRaw(t, "exp_bool", "exposure", "control", 0, today.Add(12*time.Hour))
	f.seedRaw(t, "exp_bool", "fetch", "", 2, yesterday.Add(23*time.Hour+59*time.Minute+59*time.Second))
	f.seedRollup(t, yesterday, "exp_bool", "control", 1, 2, 3)

	_, body := callStats(t, f.app, "?since=90d&flag=exp_bool")

	if got := numField(t, body, "fetches"); got != 3 {
		t.Errorf("fetches = %v, want 3 (1 raw today + 2 rolled)", got)
	}
	if got := numField(t, body, "exposures"); got != 5 {
		t.Errorf("exposures = %v, want 5 (2 raw today + 3 rolled)", got)
	}
	if got := numField(t, body, "total"); got != 8 {
		t.Errorf("total = %v, want 8", got)
	}
	eventsSrc, rollupsSrc := sourcesOf(t, body)
	if eventsSrc != 3 || rollupsSrc != 5 {
		t.Errorf("sources = %v/%v, want 3/5", eventsSrc, rollupsSrc)
	}
	if body["approximate"] != true {
		t.Errorf("approximate = %v, want true", body["approximate"])
	}
	if got := echoOf(t, body)["rollupHorizon"]; got != today.Format(time.RFC3339) {
		t.Errorf("rollupHorizon = %v, want today %v", got, today.Format(time.RFC3339))
	}
	if sf, se := seriesSums(t, body); sf != 3 || se != 5 {
		t.Errorf("series sums = %d/%d, want 3/5", sf, se)
	}
	perVariant, _ := body["perVariant"].(map[string]any)
	if perVariant["control"] != float64(5) {
		t.Errorf("perVariant = %v, want control:5", perVariant)
	}
	perVersion, _ := body["perVersion"].(map[string]any)
	if perVersion["1"] != float64(2) || perVersion["2"] != float64(1) {
		t.Errorf("perVersion = %v, want 1:2 2:1", perVersion)
	}
}

// TestStatsLegacyModeParity pins CONFIGWIRE_STATS_ROLLUPS=legacy to the
// historic numbers: identical raw seed + matching rollups produce the same
// totals/series in legacy and auto mode, while legacy reports no rollup
// contribution and stays exact.
func TestStatsLegacyModeParity(t *testing.T) {
	seed := func(t *testing.T) *rollupStatsApp {
		f := newRollupStatsApp(t)
		now := time.Now().UTC()
		today := purge.DayBucket(now)
		f.markReady(t, now)
		for i := 0; i < 6; i++ {
			day := today.AddDate(0, 0, -i)
			f.seedRaw(t, "exp_bool", "fetch", "", 1, day.Add(9*time.Hour))
			f.seedRaw(t, "exp_bool", "exposure", "control", 0, day.Add(10*time.Hour))
			if i > 0 {
				f.seedRollup(t, day, "exp_bool", "control", 1, 1, 1)
			}
		}
		return f
	}

	autoApp := seed(t)
	_, autoBody := callStats(t, autoApp.app, "?since=90d&flag=exp_bool")

	legacyApp := seed(t)
	t.Setenv(statsRollupsEnvVar, "  LeGaCy  ")
	_, legacyBody := callStats(t, legacyApp.app, "?since=90d&flag=exp_bool")

	if autoBody["fetches"] != legacyBody["fetches"] ||
		autoBody["exposures"] != legacyBody["exposures"] ||
		autoBody["total"] != legacyBody["total"] {
		t.Fatalf("legacy totals = %v/%v/%v, auto = %v/%v/%v; want parity",
			legacyBody["fetches"], legacyBody["exposures"], legacyBody["total"],
			autoBody["fetches"], autoBody["exposures"], autoBody["total"])
	}
	if !reflect.DeepEqual(autoBody["perVariant"], legacyBody["perVariant"]) ||
		!reflect.DeepEqual(autoBody["perVersion"], legacyBody["perVersion"]) {
		t.Errorf("legacy perVariant/perVersion = %v/%v, auto = %v/%v; want parity",
			legacyBody["perVariant"], legacyBody["perVersion"], autoBody["perVariant"], autoBody["perVersion"])
	}
	autoSeries, _ := json.Marshal(autoBody["series"])
	legacySeries, _ := json.Marshal(legacyBody["series"])
	if string(autoSeries) != string(legacySeries) {
		t.Errorf("legacy series != auto series\nlegacy: %s\nauto:   %s", legacySeries, autoSeries)
	}

	autoEvents, autoRollups := sourcesOf(t, autoBody)
	legacyEvents, legacyRollups := sourcesOf(t, legacyBody)
	if autoRollups != 10 || legacyRollups != 0 {
		t.Errorf("sources.rollups auto/legacy = %v/%v, want 10/0", autoRollups, legacyRollups)
	}
	if autoEvents != 2 || legacyEvents != 12 {
		t.Errorf("sources.events auto/legacy = %v/%v, want 2/12", autoEvents, legacyEvents)
	}
	if autoBody["approximate"] != true || legacyBody["approximate"] != false {
		t.Errorf("approximate auto/legacy = %v/%v, want true/false", autoBody["approximate"], legacyBody["approximate"])
	}
	legacyHorizon := LegacyHorizonFor(time.Now().UTC()).Format(time.RFC3339)
	if got := echoOf(t, legacyBody)["rollupHorizon"]; got != legacyHorizon {
		t.Errorf("legacy rollupHorizon = %v, want %v", got, legacyHorizon)
	}
}

// TestStatsReadinessFallback pins the safety gate: with no persisted
// high-water mark (or one older than yesterday) stats uses the legacy raw
// window and reports it; once ready it switches to today's horizon.
func TestStatsReadinessFallback(t *testing.T) {
	f := newRollupStatsApp(t)
	now := time.Now().UTC()
	today := purge.DayBucket(now)
	yesterday := today.AddDate(0, 0, -1)
	f.seedRaw(t, "exp_bool", "fetch", "", 1, yesterday.Add(9*time.Hour))
	f.seedRaw(t, "exp_bool", "fetch", "", 1, yesterday.Add(10*time.Hour))
	f.seedRollup(t, yesterday, "exp_bool", "control", 1, 999, 999)

	_, fallbackBody := callStats(t, f.app, "?since=90d&flag=exp_bool")
	if got := numField(t, fallbackBody, "fetches"); got != 2 {
		t.Errorf("fallback fetches = %v, want 2 (raw rows)", got)
	}
	if got := numField(t, fallbackBody, "exposures"); got != 0 {
		t.Errorf("fallback exposures = %v, want 0 (rollups not trusted)", got)
	}
	if fallbackBody["approximate"] != false {
		t.Errorf("fallback approximate = %v, want false", fallbackBody["approximate"])
	}
	legacyHorizon := LegacyHorizonFor(now).Format(time.RFC3339)
	if got := echoOf(t, fallbackBody)["rollupHorizon"]; got != legacyHorizon {
		t.Errorf("fallback rollupHorizon = %v, want %v", got, legacyHorizon)
	}

	f.markReady(t, now)
	_, readyBody := callStats(t, f.app, "?since=90d&flag=exp_bool")
	if got := numField(t, readyBody, "fetches"); got != 999 {
		t.Errorf("ready fetches = %v, want 999 (rollups trusted)", got)
	}
	if readyBody["approximate"] != true {
		t.Errorf("ready approximate = %v, want true", readyBody["approximate"])
	}
	if got := echoOf(t, readyBody)["rollupHorizon"]; got != today.Format(time.RFC3339) {
		t.Errorf("ready rollupHorizon = %v, want today %v", got, today.Format(time.RFC3339))
	}
}

// TestStatsFlagAndSourcesWithRollups pins ?flag scoping and flagFound when
// the completed-day counts come from event_daily, plus unknown-flag zeros.
func TestStatsFlagAndSourcesWithRollups(t *testing.T) {
	f := newRollupStatsApp(t)
	now := time.Now().UTC()
	today := purge.DayBucket(now)
	yesterday := today.AddDate(0, 0, -1)
	f.markReady(t, now)

	f.seedRaw(t, "exp_bool", "fetch", "", 1, today.Add(time.Hour))
	f.seedRaw(t, "other_flag", "fetch", "", 1, today.Add(2*time.Hour))
	f.seedRollup(t, yesterday, "exp_bool", "control", 1, 3, 4)
	f.seedRollup(t, yesterday, "other_flag", "treatment", 1, 5, 6)

	_, expBody := callStats(t, f.app, "?since=90d&flag=exp_bool")
	if got := numField(t, expBody, "fetches"); got != 4 {
		t.Errorf("exp_bool fetches = %v, want 4", got)
	}
	if got := numField(t, expBody, "exposures"); got != 4 {
		t.Errorf("exp_bool exposures = %v, want 4", got)
	}
	if expBody["flagFound"] != true {
		t.Errorf("exp_bool flagFound = %v, want true", expBody["flagFound"])
	}
	if eventsSrc, rollupsSrc := sourcesOf(t, expBody); eventsSrc != 1 || rollupsSrc != 7 {
		t.Errorf("exp_bool sources = %v/%v, want 1/7", eventsSrc, rollupsSrc)
	}

	_, unknownBody := callStats(t, f.app, "?since=90d&flag=no-such-flag")
	if got := numField(t, unknownBody, "total"); got != 0 {
		t.Errorf("unknown total = %v, want 0", got)
	}
	if unknownBody["flagFound"] != false {
		t.Errorf("unknown flagFound = %v, want false", unknownBody["flagFound"])
	}
	eventsSrc, rollupsSrc := sourcesOf(t, unknownBody)
	if eventsSrc != 0 || rollupsSrc != 0 {
		t.Errorf("unknown sources = %v/%v, want 0/0", eventsSrc, rollupsSrc)
	}
}
