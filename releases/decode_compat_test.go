package releases

import (
	"encoding/json"
	"testing"
)

// Both stored shapes must fold to identical in-memory sets: same per-flag
// nested rows, same flattened eval set, both validating clean.
func TestFlattenedSetsIdenticalAcrossShapes(t *testing.T) {
	legacyRaw := []byte(`{"flags":[{"key":"hero_button","type":"string","default":"blue","group":"","rules":[]},{"key":"new_checkout","type":"bool","default":false,"group":"","rules":[]}],"experiments":[{"id":"exp1","flag":"hero_button","seed":"seed-1","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"}]}`)
	nestedRaw := []byte(`{"flags":[{"key":"hero_button","type":"string","default":"blue","group":"","rules":[],"experiments":[{"id":"exp1","seed":"seed-1","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"}]},{"key":"new_checkout","type":"bool","default":false,"group":"","rules":[]}]}`)

	var legacy, nested Snapshot
	if err := json.Unmarshal(legacyRaw, &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if err := json.Unmarshal(nestedRaw, &nested); err != nil {
		t.Fatalf("nested decode: %v", err)
	}
	lflat, _ := json.Marshal(legacy.Experiments)
	nflat, _ := json.Marshal(nested.Experiments)
	if string(lflat) != string(nflat) {
		t.Fatalf("flattened sets differ:\nlegacy %s\nnested %s", lflat, nflat)
	}
	if len(nested.Experiments) != 1 || nested.Experiments[0].Flag != "hero_button" {
		t.Fatalf("nested parent key not stamped: %+v", nested.Experiments)
	}
	lnest, _ := json.Marshal(legacy.Flags)
	nnest, _ := json.Marshal(nested.Flags)
	if string(lnest) != string(nnest) {
		t.Fatalf("per-flag nested rows differ:\nlegacy %s\nnested %s", lnest, nnest)
	}
	if err := ValidateSnapshot(legacy); err != nil {
		t.Fatalf("legacy folded must validate: %v", err)
	}
	if err := ValidateSnapshot(nested); err != nil {
		t.Fatalf("nested must validate: %v", err)
	}
}

// IsLegacyTopLevel is true only for a non-empty top-level experiments
// array; nested-only, missing, empty, or malformed shapes read false.
func TestIsLegacyTopLevel(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"nonEmptyTopLevel", `{"flags":[],"experiments":[{"id":"e1"}]}`, true},
		{"missing", `{"flags":[]}`, false},
		{"empty", `{"flags":[],"experiments":[]}`, false},
		{"nestedOnly", `{"flags":[{"key":"a","experiments":[{"id":"e1"}]}]}`, false},
		{"malformed", `{not json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLegacyTopLevel([]byte(tc.raw)); got != tc.want {
				t.Fatalf("IsLegacyTopLevel(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
func TestCanonicalNestedEtag(t *testing.T) {
	var snap Snapshot
	legacyRaw := []byte(`{"flags":[{"key":"a","type":"bool","default":true,"group":"","rules":[]}],"experiments":[{"id":"exp1","flag":"a","seed":"s","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"}]}`)
	if err := json.Unmarshal(legacyRaw, &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	canonical, err := MarshalCanonical(snap)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(canonical, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["experiments"]; ok {
		t.Fatalf("canonical must emit nested shape only, got %s", canonical)
	}
	again, err := MarshalCanonical(snap)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(again) {
		t.Fatalf("canonical bytes not deterministic")
	}
	if e1, e2 := EtagFor(3, canonical), EtagFor(3, again); e1 != e2 {
		t.Fatalf("etag over canonical nested bytes not deterministic: %q vs %q", e1, e2)
	}
	if e1, e2 := EtagFor(3, canonical), EtagFor(4, canonical); e1 == e2 {
		t.Fatalf("etag must differ across versions for identical canonical bytes")
	}
}
