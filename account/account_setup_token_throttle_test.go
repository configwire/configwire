package account

import (
	"bytes"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// deniedEvent builds a RequestEvent missing the setup-token header, so
// setupTokenDenied blocks it (no app needed: the guard reads only the
// package token snapshot and the request header).
func deniedEvent() *core.RequestEvent {
	req := httptest.NewRequest("POST", "/api/v1/admin/setup", nil)
	re := &core.RequestEvent{}
	re.Request = req
	return re
}

func countDenialLines(buf *bytes.Buffer) int {
	n := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "setup denied") {
			n++
		}
	}
	return n
}

// Rapid denials inside one window emit exactly one line; the next denial
// after the window advances emits again and carries the suppressed count.
func TestSetupTokenDeniedThrottle(t *testing.T) {
	withSetupToken(t, "change-me-32-chars-test-token")

	setupDeniedMu.Lock()
	prevLast := setupDeniedLastLog
	prevSuppressed := setupDeniedSuppressed
	setupDeniedLastLog = time.Time{}
	setupDeniedSuppressed = 0
	setupDeniedMu.Unlock()
	t.Cleanup(func() {
		setupDeniedMu.Lock()
		setupDeniedLastLog = prevLast
		setupDeniedSuppressed = prevSuppressed
		setupDeniedMu.Unlock()
	})

	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	if !setupTokenDenied(deniedEvent()) {
		t.Fatal("first denial: got allowed, want denied")
	}
	if !setupTokenDenied(deniedEvent()) {
		t.Fatal("second denial: got allowed, want denied")
	}
	if got := countDenialLines(&buf); got != 1 {
		t.Fatalf("two denials in one window: got %d log lines, want 1\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), "suppressed") {
		t.Fatalf("first window line must not carry a suffix, got:\n%s", buf.String())
	}

	setupDeniedMu.Lock()
	setupDeniedLastLog = setupDeniedLastLog.Add(-2 * time.Second)
	setupDeniedMu.Unlock()
	buf.Reset()

	if !setupTokenDenied(deniedEvent()) {
		t.Fatal("post-window denial: got allowed, want denied")
	}
	if got := countDenialLines(&buf); got != 1 {
		t.Fatalf("post-window denial: got %d log lines, want 1\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "(+1 suppressed)") {
		t.Fatalf("post-window line must carry (+1 suppressed), got:\n%s", buf.String())
	}
}
