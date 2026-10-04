package releases

import (
	"errors"
	"strings"
	"testing"
)

func isPromoteLowerHex(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// promoteSameProjectGate mirrors the comparison handler.go performs:
// differing project ids produce the handler's "across projects" error.
func promoteSameProjectGate(srcProject, destProject string) error {
	if srcProject != destProject {
		return errors.New("promotion across projects is not allowed: source and destination must share one project.")
	}
	return nil
}

func TestPromoteEtagFreshOnDest(t *testing.T) {
	snap := []byte(`{"flags":[{"key":"a","type":"bool","default":true,"group":"","rules":[]}],"experiments":[]}`)
	srcEtag := EtagFor(2, snap)
	destEtag := EtagFor(5, snap)
	if srcEtag == destEtag {
		t.Fatalf("same snapshot bytes must yield fresh etag on dest version: src v2 %q == dest v5 %q", srcEtag, destEtag)
	}
	for name, etag := range map[string]string{"src v2": srcEtag, "dest v5": destEtag} {
		if !isPromoteLowerHex(etag) {
			t.Errorf("%s: etag must be 16 lowercase hex, got %q", name, etag)
		}
	}
}

func TestPromotePickSrcEnvScoped(t *testing.T) {
	tests := []struct {
		name    string
		rows    []ReleaseRow
		version int
		envID   string
		wantIdx int
		wantErr error
	}{
		{
			name:    "envA v1 resolves to index 0",
			rows:    []ReleaseRow{{EnvID: "envA", Version: 1}, {EnvID: "envB", Version: 1}},
			version: 1, envID: "envA", wantIdx: 0,
		},
		{
			name:    "envB v1 resolves to index 1",
			rows:    []ReleaseRow{{EnvID: "envA", Version: 1}, {EnvID: "envB", Version: 1}},
			version: 1, envID: "envB", wantIdx: 1,
		},
		{
			name:    "missing version in env",
			rows:    []ReleaseRow{{EnvID: "envA", Version: 1}},
			version: 1, envID: "envB", wantIdx: -1, wantErr: ErrReleaseNotFound,
		},
		{
			name:    "missing version number",
			rows:    []ReleaseRow{{EnvID: "envA", Version: 1}},
			version: 2, envID: "envA", wantIdx: -1, wantErr: ErrReleaseNotFound,
		},
		{
			name:    "duplicate rows same env and version",
			rows:    []ReleaseRow{{EnvID: "envA", Version: 1}, {EnvID: "envA", Version: 1}},
			version: 1, envID: "envA", wantIdx: -1, wantErr: ErrReleaseAmbiguous,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := RollbackPick(tc.rows, tc.version, tc.envID)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got idx=%d err=%v", tc.wantErr, idx, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if idx != tc.wantIdx {
				t.Fatalf("expected index %d, got %d", tc.wantIdx, idx)
			}
		})
	}
}

func TestPromoteSameProjectGate(t *testing.T) {
	tests := []struct {
		name        string
		srcProject  string
		destProject string
		wantErrSub  string
	}{
		{name: "equal project ids pass", srcProject: "projA", destProject: "projA"},
		{name: "differing ids rejected", srcProject: "projA", destProject: "projB", wantErrSub: "across projects"},
		{name: "empty dest rejected", srcProject: "projA", destProject: "", wantErrSub: "across projects"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := promoteSameProjectGate(tc.srcProject, tc.destProject)
			if tc.wantErrSub == "" {
				if err != nil {
					t.Fatalf("same project must pass, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("must fail with %q, got %v", tc.wantErrSub, err)
			}
		})
	}
}

func TestPromoteValidateGate(t *testing.T) {
	badWeights := func() Snapshot {
		snap := goodSnapshot()
		snap.Experiments[0].Variants = []any{
			map[string]any{"name": "control", "weightBps": float64(9900)},
		}
		return snap
	}
	badKey := func() Snapshot {
		snap := goodSnapshot()
		snap.Flags[0].Key = "1bad"
		return snap
	}
	tests := []struct {
		name       string
		snap       func() Snapshot
		wantErrSub string
	}{
		{name: "empty snapshot", snap: func() Snapshot { return Snapshot{} }, wantErrSub: "nothing to publish"},
		{name: "bad flag key", snap: badKey, wantErrSub: "invalid flag key"},
		{name: "bad experiment weights", snap: badWeights, wantErrSub: "exp1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSnapshot(tc.snap())
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErrSub, err)
			}
		})
	}
}
