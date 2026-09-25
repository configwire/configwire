package releases

// Dual-gate parity characterization for the per-project flag cap.
// Write path: configwire/main.go flags-create hook (maxFlagsPerProject).
// Publish path: ValidateSnapshot below. Both gates must reject the
// 1001st flag with a byte-identical "flag limit reached" message while
// 1000 flags pass and updates stay recount-free (update path has no
// count check by design).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantCapMessage = "flag limit reached: max 1000 flags per project"

func makeCapFlags(n int) []SnapshotFlag {
	flags := make([]SnapshotFlag, n)
	for i := range flags {
		flags[i] = SnapshotFlag{Key: fmt.Sprintf("cap_%04d", i), Type: "bool", Default: true, Rules: []SnapshotRule{}}
	}
	return flags
}

// TestFlagCapBoundary pins the publish-path cap on UNCHANGED code:
// exactly 1000 flags publish fine, the 1001st is rejected with the
// exact gate message. Must stay GREEN before and after the fix.
func TestFlagCapBoundary(t *testing.T) {
	if err := ValidateSnapshot(Snapshot{Flags: makeCapFlags(1000)}); err != nil {
		t.Fatalf("1000 flags must pass the cap gate, got: %v", err)
	}
	err := ValidateSnapshot(Snapshot{Flags: makeCapFlags(1001)})
	if err == nil {
		t.Fatal("1001 flags must breach the 1000/project cap")
	}
	if err.Error() != wantCapMessage {
		t.Fatalf("cap message must stay byte-identical, got %q want %q", err.Error(), wantCapMessage)
	}
}

// TestFlagCapWritePathFilteredCount proves the write path counts with a
// filtered COUNT query (no full-table scan). It extracts the
// countProjectFlags body from ../main.go and requires:
//   - a filtered count (CountRecords with a project filter), and
//   - no FindAllRecords full scan inside that function.
//
// RED before the fix (scan via FindAllRecords + in-Go filter);
// GREEN after (CountRecords + dbx.HashExp{"project": ...}).
func TestFlagCapWritePathFilteredCount(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "main.go"))
	if err != nil {
		t.Fatalf("read ../main.go: %v", err)
	}
	body := extractCountProjectFlagsBody(t, string(src))
	if strings.Contains(body, "FindAllRecords") {
		t.Fatalf("countProjectFlags must not full-scan via FindAllRecords; body:\n%s", body)
	}
	if !strings.Contains(body, "CountRecords") {
		t.Fatalf("countProjectFlags must use a filtered CountRecords query; body:\n%s", body)
	}
	if !strings.Contains(body, `"project"`) {
		t.Fatalf("countProjectFlags must filter by project; body:\n%s", body)
	}
}

// TestFlagCapGatesInSync pins the dual-gate parity without duplicating
// logic: both gates keep the 1000 limit and the byte-identical message.
func TestFlagCapGatesInSync(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "main.go"))
	if err != nil {
		t.Fatalf("read ../main.go: %v", err)
	}
	main := string(src)
	for _, want := range []string{
		"maxFlagsPerProject = 1000",
		`"flag limit reached: max 1000 flags per project"`,
		"countProjectFlags(e.App, e.Record.GetString(\"project\"))",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("../main.go write gate drifted, missing %q", want)
		}
	}
	if got := fmt.Sprintf("flag limit reached: max %d flags per project", maxFlagsPerProject); got != wantCapMessage {
		t.Fatalf("publish gate message drifted: got %q want %q", got, wantCapMessage)
	}
	if maxFlagsPerProject != 1000 {
		t.Fatalf("publish gate cap drifted: got %d want 1000", maxFlagsPerProject)
	}
}

// extractCountProjectFlagsBody returns the source of countProjectFlags
// (from its func line through the matching closing brace at col 0).
func extractCountProjectFlagsBody(t *testing.T, src string) string {
	t.Helper()
	start := strings.Index(src, "func countProjectFlags(")
	if start < 0 {
		t.Fatal("func countProjectFlags not found in ../main.go")
	}
	rest := src[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatal("could not find end of func countProjectFlags in ../main.go")
	}
	return rest[:end+3]
}
