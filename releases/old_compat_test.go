package releases

import (
	"encoding/json"
	"testing"
)

func TestOldTopLevelCompatParseValidate(t *testing.T) {
	raw := []byte(`{"flags":[{"key":"hero_button","type":"string","default":"blue","group":"","rules":[]},{"key":"new_checkout","type":"bool","default":false,"group":"","rules":[]}],"experiments":[{"id":"exp1","flag":"hero_button","seed":"seed-1","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"}]}`)
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("old top-level snapshot must unmarshal: %v", err)
	}
	if len(snap.Flags) != 2 {
		t.Fatalf("old snapshot decoded to %d flags, want 2", len(snap.Flags))
	}
	var hero *SnapshotFlag
	for i := range snap.Flags {
		if snap.Flags[i].Key == "hero_button" {
			hero = &snap.Flags[i]
		}
	}
	if hero == nil || len(hero.Experiments) != 1 {
		t.Fatalf("hero_button must fold 1 nested exp, got %+v", snap.Flags)
	}
	if hero.Experiments[0].ID != "exp1" || hero.Experiments[0].Seed != "seed-1" {
		t.Fatalf("folded exp wrong: %+v", hero.Experiments[0])
	}
	if err := ValidateSnapshot(snap); err != nil {
		t.Fatalf("folded old snapshot must validate ok: %v", err)
	}
	out, err := MarshalCanonical(snap)
	if err != nil {
		t.Fatal(err)
	}
	var canon map[string]any
	if err := json.Unmarshal(out, &canon); err != nil {
		t.Fatal(err)
	}
	topExps, ok := canon["experiments"].([]any)
	if !ok || len(topExps) != 1 {
		t.Fatalf("canonical must dual-emit one top-level experiment, got %s", out)
	}
	topRow, ok := topExps[0].(map[string]any)
	if !ok || topRow["id"] != "exp1" || topRow["flag"] != "hero_button" || topRow["seed"] != "seed-1" {
		t.Fatalf("dual top-level row must stamp parent flag key, got %s", out)
	}
}

func TestOldTopLevelCompatOrphansDropped(t *testing.T) {
	raw := []byte(`{"flags":[{"key":"hero_button","type":"string","default":"blue","group":"","rules":[]}],"experiments":[{"id":"exp1","flag":"hero_button","seed":"s1","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"},{"id":"orphan","flag":"ghost","seed":"s2","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"}]}`)
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal old with orphan: %v", err)
	}
	if len(snap.Flags) != 1 || len(snap.Flags[0].Experiments) != 1 {
		t.Fatalf("orphan must be dropped, got %+v", snap.Flags)
	}
	if snap.Flags[0].Experiments[0].ID != "exp1" {
		t.Fatalf("wrong exp kept: %+v", snap.Flags[0].Experiments[0])
	}
	if err := ValidateSnapshot(snap); err != nil {
		t.Fatalf("folded parent snapshot must validate ok: %v", err)
	}
}

func TestNestedShapeFlagsPreservedValidateOk(t *testing.T) {
	raw := []byte(`{"flags":[{"key":"hero_button","type":"string","default":"blue","group":"","rules":[],"experiments":[{"id":"exp1","seed":"seed-1","variants":[{"name":"control","weightBps":5000},{"name":"treatment","weightBps":5000}],"status":"draft"}]}]}`)
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("nested snapshot must unmarshal: %v", err)
	}
	if len(snap.Flags) != 1 || snap.Flags[0].Key != "hero_button" {
		t.Fatalf("nested flags not preserved: %+v", snap.Flags)
	}
	if len(snap.Flags[0].Experiments) != 1 || snap.Flags[0].Experiments[0].ID != "exp1" {
		t.Fatalf("nested exps not preserved: %+v", snap.Flags[0].Experiments)
	}
	if err := ValidateSnapshot(snap); err != nil {
		t.Fatalf("nested snapshot must validate ok: %v", err)
	}
}
