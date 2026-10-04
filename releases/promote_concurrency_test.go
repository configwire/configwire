package releases

// Concurrency proof for promote under writeMu: two simultaneous promotes
// to the SAME dest env with the SAME destBaseVersion must yield exactly
// one 200 + one 409 (no oversell), mirroring the publish double-submit
// guarantee documented on writeMu.
//
// Lock-scope citation (configwire/releases/handler.go):
//   - handler.go:25-31 — writeMu comment: publish AND promote assign
//     version=max+1 per env; the mutex turns a concurrent double-submit
//     with the same baseVersion into exactly one 200 + one 409 (the
//     loser re-reads the fresh max under the lock). Cross-process
//     serialization is out of scope: ConfigWire runs as one binary
//     (single-binary scope documented here too).
//   - handler.go:343-385 (promoteToNewRow comment) — the helper does NOT
//     lock; the caller holds writeMu.
//   - handler.go:491-492 — postPromoteEnv takes ONE writeMu.Lock/Unlock.
//   - handler.go:494-520 — that single critical section wraps
//     FindAllRecords + RollbackPick (src lookup) + same-project check +
//     MaxVersionForEnv dest-max re-read + CheckBaseVersion 409 (NO write)
//     + promoteToNewRow insert (internalSave). Because the dest-max
//     re-read sits INSIDE the lock, the loser observes the winner's row
//     and answers 409 with currentVersion instead of writing.
//
// This test drives the REAL postPromoteEnv handler (not a reimplementation
// of its guard) through fabricated *core.RequestEvent values backed by
// httptest recorders, so the package-level writeMu is genuinely contended.
// Slug resolution runs outside the lock (as in the handler); both
// goroutines resolve the same dev->prod pair in the same project.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
)

// promoteConcurrencySeed is a minimal valid snapshot for the dev source
// row: one bool flag, no rules, no experiments. It must pass
// ValidateSnapshot because promoteToNewRow re-validates the cloned bytes.
const promoteConcurrencySeed = `{"flags":[{"key":"promo_flag","type":"bool","default":true,"group":"","rules":[]}],"experiments":[]}`

// promoteConcurrencyApp builds a TestApp with the collections postPromoteEnv
// touches: environments (slug/project, for envresolve.Resolve) and releases
// (version number + snapshot JSON + env text, mirroring the real migration
// shape closely enough for MaxVersionForEnv/RollbackPick/promoteToNewRow).
func promoteConcurrencyApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	envs := core.NewBaseCollection("environments")
	envs.Fields.Add(
		&core.TextField{Name: "slug"},
		&core.TextField{Name: "project"},
	)
	if err := app.Save(envs); err != nil {
		t.Fatalf("save environments collection: %v", err)
	}

	rels := core.NewBaseCollection("releases")
	rels.Fields.Add(
		&core.NumberField{Name: "version", OnlyInt: true},
		&core.TextField{Name: "etag"},
		&core.JSONField{Name: "snapshot"},
		&core.TextField{Name: "author"},
		&core.TextField{Name: "note"},
		&core.TextField{Name: "env"},
	)
	if err := app.Save(rels); err != nil {
		t.Fatalf("save releases collection: %v", err)
	}
	return app
}

// promoteConcurrencyEnv creates one environment row and returns its id.
func promoteConcurrencyEnv(t *testing.T, app *tests.TestApp, slug, project string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", slug)
	rec.Set("project", project)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env %s: %v", slug, err)
	}
	return rec.Id
}

// invokePromote calls the real postPromoteEnv handler for destSlug with the
// given promote body and returns the HTTP status it wrote plus the raw
// JSON body. Each caller must pass its own recorder (one per goroutine).
func invokePromote(app *tests.TestApp, destSlug, body string) (int, []byte, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/env/"+destSlug+"/promote", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("dest", destSlug)
	re := &core.RequestEvent{
		App:   app,
		Event: router.Event{Response: rec, Request: req},
	}
	if err := postPromoteEnv(re); err != nil {
		return 0, nil, err
	}
	return rec.Code, rec.Body.Bytes(), nil
}

func TestPromoteConcurrent(t *testing.T) {
	app := promoteConcurrencyApp(t)

	// Given: one project with dev/prod envs and a dev v1 source release
	// (the promote source, published as version 1).
	const project = "proj-conc"
	promoteConcurrencyEnv(t, app, "dev", project)
	promoteConcurrencyEnv(t, app, "prod", project)
	devRec, err := app.FindRecordsByFilter("environments", "slug = {:slug}", "", 1, 0, map[string]any{"slug": "dev"})
	if err != nil || len(devRec) != 1 {
		t.Fatalf("resolve dev env: %v (%d rows)", err, len(devRec))
	}
	prodRec, err := app.FindRecordsByFilter("environments", "slug = {:slug}", "", 1, 0, map[string]any{"slug": "prod"})
	if err != nil || len(prodRec) != 1 {
		t.Fatalf("resolve prod env: %v (%d rows)", err, len(prodRec))
	}
	prodID := prodRec[0].Id
	srcCol, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	src := core.NewRecord(srcCol)
	src.Set("version", 1)
	src.Set("etag", EtagFor(1, []byte(promoteConcurrencySeed)))
	src.Set("snapshot", promoteConcurrencySeed)
	src.Set("author", "test")
	src.Set("note", "dev v1")
	src.Set("env", devRec[0].Id)
	if err := internalSave(app, src); err != nil {
		t.Fatalf("seed dev v1: %v", err)
	}

	destMaxBefore, err := MaxVersionForEnv(app, prodID)
	if err != nil {
		t.Fatalf("dest max before: %v", err)
	}
	if destMaxBefore != 0 {
		t.Fatalf("prod must start empty, max=%d", destMaxBefore)
	}

	// When: two goroutines promote the SAME src version to the SAME dest
	// with the SAME destBaseVersion (0 on the empty prod), released
	// through a start gate so both contend inside postPromoteEnv.
	// Structure mirrors TestPublishOnlyGuardConcurrentRace (WaitGroup +
	// result channel); the gate additionally forces true overlap for -race.
	const racers = 2
	body := `{"srcEnv":"dev","srcVersion":1,"note":"conc","destBaseVersion":0}`
	type outcome struct {
		code           int
		version        int
		currentVersion int
		callErr        error
	}
	outCh := make(chan outcome, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, raw, callErr := invokePromote(app, "prod", body)
			o := outcome{code: code, callErr: callErr}
			if callErr == nil {
				var parsed struct {
					Version        int    `json:"version"`
					CurrentVersion int    `json:"currentVersion"`
					Etag           string `json:"etag"`
				}
				if err := json.Unmarshal(raw, &parsed); err != nil {
					o.callErr = fmt.Errorf("decode promote response %q: %w", raw, err)
				} else {
					o.version = parsed.Version
					o.currentVersion = parsed.CurrentVersion
				}
			}
			outCh <- o
		}()
	}
	close(start)
	wg.Wait()
	close(outCh)

	var outcomes []outcome
	for o := range outCh {
		if o.callErr != nil {
			t.Fatalf("promote call failed: %v", o.callErr)
		}
		outcomes = append(outcomes, o)
	}
	if len(outcomes) != racers {
		t.Fatalf("want %d outcomes, got %d", racers, len(outcomes))
	}

	// Then: the multiset of codes is exactly {200, 409} — one winner, one
	// loser that saw the fresh max under the lock.
	codes := []int{outcomes[0].code, outcomes[1].code}
	sort.Ints(codes)
	if codes[0] != http.StatusOK || codes[1] != http.StatusConflict {
		t.Fatalf("want exactly one 200 + one 409, got %v", codes)
	}
	var winner, loser outcome
	if outcomes[0].code == http.StatusOK {
		winner, loser = outcomes[0], outcomes[1]
	} else {
		winner, loser = outcomes[1], outcomes[0]
	}
	if winner.version != 1 {
		t.Fatalf("winner must create dest version 1, got %d", winner.version)
	}
	if loser.currentVersion != 1 {
		t.Fatalf("loser 409 must carry currentVersion=1, got %d", loser.currentVersion)
	}

	// Then: dest max advanced by exactly 1 (no oversell, no missing write).
	destMaxAfter, err := MaxVersionForEnv(app, prodID)
	if err != nil {
		t.Fatalf("dest max after: %v", err)
	}
	if destMaxAfter != destMaxBefore+1 {
		t.Fatalf("dest max must advance by exactly 1 (%d -> %d), got %d", destMaxBefore, destMaxBefore+1, destMaxAfter)
	}

	// Then: no two rows share (env, version); the dest env holds exactly
	// the single winner row at version 1 (strictly sequential).
	recs, err := app.FindAllRecords("releases")
	if err != nil {
		t.Fatalf("FindAllRecords: %v", err)
	}
	seen := map[string]bool{}
	prodCount := 0
	for _, r := range recs {
		key := r.GetString("env") + "\x00" + fmt.Sprint(r.GetInt("version"))
		if seen[key] {
			t.Fatalf("duplicate (env, version) row: env=%q version=%d", r.GetString("env"), r.GetInt("version"))
		}
		seen[key] = true
		if r.GetString("env") == prodID {
			prodCount++
			if v := r.GetInt("version"); v != 1 {
				t.Fatalf("prod row must be version 1, got %d", v)
			}
		}
	}
	if prodCount != 1 {
		t.Fatalf("prod must hold exactly 1 release row, got %d", prodCount)
	}
}
