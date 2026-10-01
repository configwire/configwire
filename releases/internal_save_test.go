package releases

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// guardTestApp builds a TestApp with a minimal releases collection and the
// same publish-only create/delete guards as configwire/main.go: unmarked
// saves are rejected, marked (internalSave) saves pass.
func guardTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("releases")
	for _, f := range []string{
		`{"type":"text","name":"version"}`,
		`{"type":"text","name":"etag"}`,
		`{"type":"text","name":"snapshot"}`,
		`{"type":"text","name":"author"}`,
		`{"type":"text","name":"note"}`,
		`{"type":"text","name":"env"}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save releases collection: %v", err)
	}

	publishOnly := apis.NewBadRequestError("releases are publish-only: use publish/rollback endpoints", nil)
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
	return app
}

func newReleasesRecord(t *testing.T, app *tests.TestApp, version string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("version", version)
	rec.Set("etag", "etag-"+version)
	rec.Set("snapshot", `{"flags":[],"experiments":[]}`)
	rec.Set("author", "test")
	rec.Set("note", "test")
	rec.Set("env", "envA")
	return rec
}

func TestPublishOnlyGuardDirectRejectedInternalAllowed(t *testing.T) {
	app := guardTestApp(t)

	// Given: a direct save with a bare context (the data-API path shape)
	// When: saved
	// Then: the create hook rejects it with the publish-only error.
	direct := newReleasesRecord(t, app, "direct-1")
	if err := app.SaveWithContext(context.Background(), direct); err == nil || !strings.Contains(err.Error(), "publish-only") {
		t.Fatalf("direct SaveWithContext must be rejected with publish-only, got err=%v", err)
	}
	if err := app.Save(newReleasesRecord(t, app, "direct-2")); err == nil || !strings.Contains(err.Error(), "publish-only") {
		t.Fatalf("direct Save must be rejected with publish-only, got err=%v", err)
	}

	// Given: a save through internalSave (the publish/rollback path shape)
	// When: saved
	// Then: the create hook allows it.
	kept := newReleasesRecord(t, app, "internal-1")
	if err := internalSave(app, kept); err != nil {
		t.Fatalf("internalSave must be allowed, got err=%v", err)
	}

	// Given: a save with an explicitly marked context
	// When: saved
	// Then: the create hook allows it.
	marked := newReleasesRecord(t, app, "internal-2")
	if err := app.SaveWithContext(markInternal(context.Background()), marked); err != nil {
		t.Fatalf("marked-context save must be allowed, got err=%v", err)
	}

	// Given: an existing releases row
	// When: deleted directly
	// Then: the delete hook rejects it with the publish-only error.
	if err := app.Delete(kept); err == nil || !strings.Contains(err.Error(), "publish-only") {
		t.Fatalf("direct Delete must be rejected with publish-only, got err=%v", err)
	}
}

func TestPublishOnlyGuardConcurrentRace(t *testing.T) {
	app := guardTestApp(t)

	// Given: goroutines racing direct data-API saves against internalSave,
	// looping enough times to have hit the old global-counter race window
	// (any direct save landing while an internal save was in flight).
	// When: run concurrently under -race
	// Then: every direct save is rejected and every internal save succeeds.
	const goroutines = 8
	const iters = 25

	var directRejected atomic.Int64
	var internalOK atomic.Int64
	errCh := make(chan string, goroutines*iters*2)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				tag := fmt.Sprintf("g%d-i%d", g, i)
				if err := app.Save(newReleasesRecord(t, app, "direct-"+tag)); err == nil || !strings.Contains(err.Error(), "publish-only") {
					errCh <- fmt.Sprintf("direct save %s: want publish-only rejection, got err=%v", tag, err)
				} else {
					directRejected.Add(1)
				}
				if err := internalSave(app, newReleasesRecord(t, app, "internal-"+tag)); err != nil {
					errCh <- fmt.Sprintf("internalSave %s: want success, got err=%v", tag, err)
				} else {
					internalOK.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}
	if got := directRejected.Load(); got != goroutines*iters {
		t.Errorf("want %d rejected direct saves, got %d", goroutines*iters, got)
	}
	if got := internalOK.Load(); got != goroutines*iters {
		t.Errorf("want %d successful internal saves, got %d", goroutines*iters, got)
	}
}
