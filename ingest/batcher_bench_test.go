package ingest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// flushBenchApp builds a TestApp with an events collection mirroring the
// flusher's write shape; relations stay plain text because the flusher
// only Sets them. Poison rows fail Save via the "poison-" create hook.
func flushBenchApp(t testing.TB) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	events := core.NewBaseCollection("events")
	for _, f := range []string{
		`{"type":"text","name":"env"}`,
		`{"type":"text","name":"flag"}`,
		`{"type":"text","name":"kind"}`,
		`{"type":"text","name":"variant"}`,
		`{"type":"text","name":"bucket"}`,
		`{"type":"text","name":"country"}`,
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

	app.OnRecordCreate("events").BindFunc(func(e *core.RecordEvent) error {
		if strings.HasPrefix(e.Record.GetString("variant"), "poison-") {
			return errors.New("batcher-chunked-test: forced poison-row failure")
		}
		return e.Next()
	})
	return app
}

// BenchmarkFlush measures one 500-row flush (the FlushSize bound) so the
// chunked-transaction change records a before/after delta in evidence.
// `go test ./ingest/ -bench BenchmarkFlush -benchtime=100x`
func BenchmarkFlush(b *testing.B) {
	app := flushBenchApp(b)
	items := make([]StoredEvent, 0, FlushSize)
	now := time.Now().UTC()
	for i := 0; i < FlushSize; i++ {
		items = append(items, StoredEvent{
			Kind: "fetch", Variant: "control",
			Ts: now, Version: 1,
		})
	}
	bt := NewBatcher(app)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bt.flush(items)
	}
}
