package releases

// Unlimited-flags characterization: no per-project flag cap exists on
// any path. The publish gate (ValidateSnapshot below) must accept flag
// counts at, just above, and well above the retired per-project cap.
// Key-shape (1-128, regex), type, and weight validations are unchanged
// and stay pinned by releases_test.go.

import (
	"fmt"
	"testing"
)

func makeManyFlags(n int) []SnapshotFlag {
	flags := make([]SnapshotFlag, n)
	for i := range flags {
		flags[i] = SnapshotFlag{Key: fmt.Sprintf("many_%05d", i), Type: "bool", Default: true, Rules: []SnapshotRule{}}
	}
	return flags
}

// TestFlagCapUnlimited pins the NEW behavior: flag volume alone never
// rejects a snapshot — 1000, 1001, and 5000 flags all publish fine.
func TestFlagCapUnlimited(t *testing.T) {
	for _, n := range []int{1000, 1001, 5000} {
		if err := ValidateSnapshot(Snapshot{Flags: makeManyFlags(n)}); err != nil {
			t.Fatalf("%d flags must pass with no per-project cap, got: %v", n, err)
		}
	}
}
