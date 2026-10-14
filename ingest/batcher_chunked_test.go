package ingest

// TDD (todo 1): chunked transactional flush with poison-row isolation.
//
// RED-before: this file references Batcher.chunkHook (the per-chunk test
// seam) and asserts batcher.go routes flush through RunInTransaction —
// both absent pre-fix, so the package fails to build / the gate fails.
// GREEN-after: flush() splits via SplitBatch(items, 250), persists each
// chunk in one RunInTransaction, and on chunk-txn failure retries rows
// individually inside that chunk (never whole-chunk-skip).

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tests"
)

// flushTestApp wraps flushBenchApp (defined in batcher_bench_test.go);

func flushTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	return flushBenchApp(t)
}

func mkFlushItems(n int) []StoredEvent {
	now := time.Now().UTC()
	out := make([]StoredEvent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, StoredEvent{
			Kind: "fetch", Variant: "control",
			UserHash: "aaaabbbbccccdddd", Ts: now, Version: 1,
		})
	}
	return out
}

// poisonItem is the oversized-variant poison row: its variant carries a
// 70KB payload (the 413-class shape at the handler) and the "poison-"
// prefix the create hook rejects, so app.Save fails deterministically.
func poisonItem() StoredEvent {
	return StoredEvent{
		Kind: "fetch", Variant: "poison-" + strings.Repeat("v", 70<<10),
		UserHash: "aaaabbbbccccdddd", Ts: time.Now().UTC(), Version: 1,
	}
}

func countFlushEvents(t *testing.T, app *tests.TestApp) int {
	t.Helper()
	recs, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	return len(recs)
}

// TestFlushUsesChunkedTransactions is the RED-before gate: flush must
// persist via RunInTransaction chunked with SplitBatch (never 500
// individual autocommit Saves, never a whole-chunk skip).
func TestFlushUsesChunkedTransactions(t *testing.T) {
	body, err := os.ReadFile("batcher.go")
	if err != nil {
		t.Fatalf("read batcher.go: %v", err)
	}
	src := string(body)
	if !strings.Contains(src, "RunInTransaction") {
		t.Fatal("flush must persist chunks via RunInTransaction (row-by-row autocommit Saves)")
	}
	if !strings.Contains(src, "SplitBatch(items, 250)") {
		t.Fatal("flush must split via SplitBatch(items, 250)")
	}
}

// TestFlushChunkedPoisonSkip: 500 good rows + 1 poison row → 499
// committed, failed=1 via total accounting, and exactly 3 chunk txns
// (250/250/1) — the chunk holding the poison still commits its 249 good
// rows (row-retry-inside-chunk, never whole-chunk-skip).
func TestFlushChunkedPoisonSkip(t *testing.T) {
	app := flushTestApp(t)
	b := NewBatcher(app)

	items := append(mkFlushItems(500), poisonItem())

	var chunks []int
	b.chunkHook = func(chunk []StoredEvent) error {
		chunks = append(chunks, len(chunk))
		return nil
	}

	before := b.total
	b.flush(items)

	if got := countFlushEvents(t, app); got != 500 {
		t.Fatalf("committed events = %d, want 500 (499 good + poison skipped, 250-chunk never dropped whole)", got)
	}
	if got := b.total - before; got != 500 {
		t.Fatalf("total delta = %d, want 500 (accepted = len-failed)", got)
	}
	if len(chunks) != 3 || chunks[0] != 250 || chunks[1] != 250 || chunks[2] != 1 {
		t.Fatalf("chunk sizes = %v, want [250 250 1]", chunks)
	}
}

// TestFlushChunkBoundaries: 501 good rows → 3 chunk txns (250/250/1),
// all 501 committed, total+=501 (happy-path evidence).
func TestFlushChunkBoundaries(t *testing.T) {
	app := flushTestApp(t)
	b := NewBatcher(app)

	var chunks []int
	b.chunkHook = func(chunk []StoredEvent) error {
		chunks = append(chunks, len(chunk))
		return nil
	}

	before := b.total
	b.flush(mkFlushItems(501))

	if got := countFlushEvents(t, app); got != 501 {
		t.Fatalf("committed events = %d, want 501", got)
	}
	if got := b.total - before; got != 501 {
		t.Fatalf("total delta = %d, want 501", got)
	}
	if len(chunks) != 3 || chunks[0] != 250 || chunks[1] != 250 || chunks[2] != 1 {
		t.Fatalf("chunk sizes = %v, want [250 250 1]", chunks)
	}
}

// TestFlushEmptyNoop: empty flush issues no chunk txn and leaves total
// untouched.
func TestFlushEmptyNoop(t *testing.T) {
	app := flushTestApp(t)
	b := NewBatcher(app)

	calls := 0
	b.chunkHook = func(chunk []StoredEvent) error {
		calls++
		return nil
	}

	before := b.total
	b.flush(nil)
	b.flush([]StoredEvent{})

	if calls != 0 {
		t.Fatalf("empty flush issued %d chunk txns, want 0", calls)
	}
	if b.total != before {
		t.Fatalf("empty flush moved total by %d, want 0", b.total-before)
	}
}

// TestFlushChunkTxnFailureRetriesRows: the first chunk txn is injected to
// fail once; the flusher must retry that chunk's rows individually — all
// 249 good rows of the failed chunk still commit, the poison row is
// skipped, total = 499. A whole-chunk-skip would commit only 250.
func TestFlushChunkTxnFailureRetriesRows(t *testing.T) {
	app := flushTestApp(t)
	b := NewBatcher(app)

	// Poison sits inside the FIRST 250-chunk: 249 good + 1 poison, then
	// 250 more good rows in the second chunk.
	items := append(mkFlushItems(249), poisonItem())
	items = append(items, mkFlushItems(250)...)

	failures := 1
	var chunks []int
	b.chunkHook = func(chunk []StoredEvent) error {
		chunks = append(chunks, len(chunk))
		if failures > 0 {
			failures--
			return errors.New("batcher-chunked-test: injected chunk-txn failure")
		}
		return nil
	}

	before := b.total
	b.flush(items)

	if got := countFlushEvents(t, app); got != 499 {
		t.Fatalf("committed events = %d, want 499 (injected txn failure must not drop the 249 good rows)", got)
	}
	if got := b.total - before; got != 499 {
		t.Fatalf("total delta = %d, want 499", got)
	}
	if len(chunks) != 2 || chunks[0] != 250 || chunks[1] != 250 {
		t.Fatalf("chunk sizes = %v, want [250 250]", chunks)
	}
}
