package purge

import (
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"

	"github.com/configwire/configwire/logstore"
)

// serverLogsTestApp extends parityTestApp with a server_logs collection
// shaped for retention: created is a plain date field (not autodate) so
// the test can backdate rows past the live logstore window.
func serverLogsTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app := parityTestApp(t)
	col := core.NewBaseCollection("server_logs")
	for _, f := range []string{
		`{"type":"number","name":"level","onlyInt":true}`,
		`{"type":"text","name":"message"}`,
		`{"type":"text","name":"src"}`,
		`{"type":"date","name":"created"}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s to server_logs: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save server_logs collection: %v", err)
	}
	return app
}

func seedServerLog(t *testing.T, app *tests.TestApp, created time.Time) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("server_logs")
	if err != nil {
		t.Fatalf("find server_logs: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("level", 1)
	rec.Set("message", "test line")
	rec.Set("src", "configwire")
	rec.Set("created", created)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save server log: %v", err)
	}
	return rec.Id
}

// TestServerLogsWiredIntoTicker pins the wiring: an outdated server_logs
// row (strictly older than now minus the live logstore MaxDays) dies via
// the daily ticker boot run while a fresh row survives, and the dry reader
// reports the doomed count with zero writes.
func TestServerLogsWiredIntoTicker(t *testing.T) {
	app := serverLogsTestApp(t)
	maxDays := logstore.GetConfig().MaxDays
	now := time.Now().UTC()
	oldID := seedServerLog(t, app, now.AddDate(0, 0, -(maxDays+1)))
	freshID := seedServerLog(t, app, now)

	dry, err := logstore.Count(app)
	if err != nil {
		t.Fatalf("logstore.Count: %v", err)
	}
	if dry != 1 {
		t.Fatalf("dry count = %d, want 1 (outdated row only)", dry)
	}
	if got := len(liveIDs(t, app, "server_logs")); got != 2 {
		t.Fatalf("rows after dry Count = %d, want 2 (zero writes)", got)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { dailyTicker(app, stop); close(done) }()

	deadline := time.Now().Add(10 * time.Second)
	for liveIDs(t, app, "server_logs")[oldID] {
		if time.Now().After(deadline) {
			t.Fatal("ticker boot run did not purge the outdated server_logs row")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ticker did not stop after close")
	}
	if !liveIDs(t, app, "server_logs")[freshID] {
		t.Fatal("fresh server_logs row wrongly purged")
	}
	if n, err := logstore.Count(app); err != nil || n != 0 {
		t.Fatalf("post-run Count = %d, err=%v; want 0 (converges)", n, err)
	}
}
