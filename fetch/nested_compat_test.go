package fetch

import (
	"encoding/json"
	"testing"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/releases"
)

// Legacy top-level shape and its nested twin must decode to identical
// eval structs: same values, same variants, no behavior change. The twin
// carries a running experiment so the overlay path (not just rules) is
// covered.
func TestEvaluateSnapshotNestedLegacyIdentical(t *testing.T) {
	legacyRaw := []byte(`{"flags":[{"key":"new_ui","type":"bool","default":false,"group":"","rules":[]}],"experiments":[{"id":"exp1","flag":"new_ui","seed":"seed-1","variants":[{"name":"control","weightBps":5000,"values":{"new_ui":false}},{"name":"treatment","weightBps":5000,"values":{"new_ui":true}}],"status":"running"}]}`)
	nestedRaw := []byte(`{"flags":[{"key":"new_ui","type":"bool","default":false,"group":"","rules":[],"experiments":[{"id":"exp1","seed":"seed-1","variants":[{"name":"control","weightBps":5000,"values":{"new_ui":false}},{"name":"treatment","weightBps":5000,"values":{"new_ui":true}}],"status":"running"}]}]}`)

	var legacy, nested releases.Snapshot
	if err := json.Unmarshal(legacyRaw, &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if err := json.Unmarshal(nestedRaw, &nested); err != nil {
		t.Fatalf("nested decode: %v", err)
	}
	ctx := eval.Context{UserHash: "0123456789abcdef"}
	lv, lvr := EvaluateSnapshot(legacy, ctx, "")
	nv, nvr := EvaluateSnapshot(nested, ctx, "")
	lj, _ := json.Marshal(map[string]any{"values": lv, "variants": lvr})
	nj, _ := json.Marshal(map[string]any{"values": nv, "variants": nvr})
	if string(lj) != string(nj) {
		t.Fatalf("nested/legacy eval differ:\nlegacy %s\nnested %s", lj, nj)
	}
	if _, ok := nvr["new_ui"]; !ok {
		t.Fatalf("nested exp must target new_ui, variants=%v", nvr)
	}
	// Anonymous ctx: both shapes fall through to the base default.
	lv, lvr = EvaluateSnapshot(legacy, eval.Context{}, "")
	nv, nvr = EvaluateSnapshot(nested, eval.Context{}, "")
	if lv["new_ui"] != false || nv["new_ui"] != false {
		t.Fatalf("anonymous must fall through to default false: %v vs %v", lv["new_ui"], nv["new_ui"])
	}
	if lvr["new_ui"] != "" || nvr["new_ui"] != "" {
		t.Fatalf("anonymous variant must be DefaultVariant: %v vs %v", lvr, nvr)
	}
}

// expsForFlag works from the compat-flattened set: nested entries resolve
// to the parent key; unknown keys (including folded-away orphans) match
// nothing.
func TestExpsForFlagNestedSet(t *testing.T) {
	var snap releases.Snapshot
	raw := []byte(`{"flags":[{"key":"a","type":"bool","default":false,"group":"","rules":[],"experiments":[{"id":"exp1","seed":"s","variants":[],"status":"running"}]}],"experiments":[{"id":"orphan","flag":"ghost","seed":"s","variants":[],"status":"running"}]}`)
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	all := flattenForEval(snap)
	if got := expsForFlag(all, "a"); len(got) != 1 || got[0].ID != "exp1" || got[0].Flag != "a" {
		t.Fatalf("nested exp must resolve to parent a: %+v", got)
	}
	if got := expsForFlag(all, "ghost"); len(got) != 0 {
		t.Fatalf("orphan flag must match nothing: %+v", got)
	}
	if got := expsForFlag(all, "missing"); len(got) != 0 {
		t.Fatalf("unknown flag must match nothing: %+v", got)
	}
}
