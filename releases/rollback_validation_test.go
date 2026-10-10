package releases

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// rollbackSrcRecord builds an unsaved releases row carrying snapshot.
// rollbackToNewRow only reads src, so the corrupt/invalid-shape cases
// need no seeding: they fail before any DB access.
func rollbackSrcRecord(t *testing.T, app *tests.TestApp, snapshot string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("releases")
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("env", "envA")
	rec.Set("version", 1)
	rec.Set("etag", "abc")
	rec.Set("snapshot", snapshot)
	return rec
}

// TestRollbackRejectsCorruptSnapshot pins that rollback re-validates
// like promote: a non-JSON source snapshot fails instead of cloning
// bad bytes into a new release.
func TestRollbackRejectsCorruptSnapshot(t *testing.T) {
	app := promoteHandlerTestApp(t)
	src := rollbackSrcRecord(t, app, "not-json{{{")
	if _, _, err := rollbackToNewRow(app, src, "note", "tester", 1); err == nil {
		t.Fatal("rollback of corrupt snapshot succeeded, want refusal")
	} else if !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("rollback error = %q, want it to mention corruption", err)
	}
}

// TestRollbackRejectsInvalidSnapshot pins that a well-formed but
// invalid snapshot (one publish and promote would both reject) cannot
// be resurrected through rollback into a new release.
func TestRollbackRejectsInvalidSnapshot(t *testing.T) {
	app := promoteHandlerTestApp(t)
	src := rollbackSrcRecord(t, app, `{"flags":[],"experiments":[]}`)
	if _, _, err := rollbackToNewRow(app, src, "note", "tester", 1); err == nil {
		t.Fatal("rollback of invalid snapshot succeeded, want refusal")
	} else if !strings.Contains(err.Error(), "nothing to publish") {
		t.Fatalf("rollback error = %q, want the ValidateSnapshot refusal", err)
	}
}

// TestDecodeRollbackNoteBodyShapes pins chunked-safe note decoding: an
// empty body means "no note" however it is framed (zero length, nil
// body, or chunked with no bytes — the old length check 400'd the
// chunked-empty case); only non-empty, non-JSON bodies are 400.
func TestDecodeRollbackNoteBodyShapes(t *testing.T) {
	mk := func(body string, contentLength int64, chunkedEmpty bool) (*core.RequestEvent, *rollbackRequest) {
		var req *http.Request
		if chunkedEmpty {
			req = httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
			req.ContentLength = contentLength
		} else if body == "" && contentLength == 0 {
			req = httptest.NewRequest(http.MethodPost, "/x", nil)
			req.Body = nil
		} else {
			req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		re := &core.RequestEvent{}
		re.Request = req
		return re, &rollbackRequest{}
	}

	re, req := mk("", 0, false)
	if err := decodeRollbackNote(re, req); err != nil || req.Note != "" {
		t.Fatalf("nil body: err=%v note=%q, want nil error and empty note", err, req.Note)
	}
	re, req = mk("", -1, true)
	if err := decodeRollbackNote(re, req); err != nil || req.Note != "" {
		t.Fatalf("chunked empty body: err=%v note=%q, want nil error and empty note", err, req.Note)
	}
	re, req = mk(`{"note":"hi"}`, 13, false)
	if err := decodeRollbackNote(re, req); err != nil || req.Note != "hi" {
		t.Fatalf("valid body: err=%v note=%q, want parsed note", err, req.Note)
	}
	re, req = mk(`{{{`, 3, false)
	if err := decodeRollbackNote(re, req); err == nil {
		t.Fatal("garbage body succeeded, want 400")
	}
}
