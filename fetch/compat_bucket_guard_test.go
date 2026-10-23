package fetch

// T8 backward-compat regression guards: old clients (no ?b=) and frozen
// wire rules behave byte-identically in the bucket era. Additive only —
// no existing test assertion is touched here.
//
// Already pinned elsewhere (referenced, not duplicated):
//   - ?uid=x -> 400 even with valid ?b= : TestBucketWithUidStill400UidWins
//   - empty release omits bucket/version-0/etag-none shape: TestEmptyReleaseBodyOmitsBucket, TestAuthOrder_EmptyReleaseShape
//   - honored-?userHash=/anonID selection without ?b=: TestBucketAbsentFallsBackToHonoredHashThenAnon
//   - ?country= ignored without ?b=: TestBuildContextCountryServerOnly
//   - nested/legacy fold without ?b=: TestEvaluateSnapshotNestedLegacyIdentical, TestOldTopLevelCompatDecodesSame
//   - 304 exact-etag without ?b=: TestAuthOrder_EtagExactMatchIs304
//   - DPA/PRIVACY: no test asserts new legal wording (docs untouched).

import (
	"encoding/json"
	"testing"

	"github.com/configwire/configwire/eval"
	"github.com/configwire/configwire/releases"
)

func decodeSnapshots(t *testing.T, legacyRaw, nestedRaw []byte) (releases.Snapshot, releases.Snapshot) {
	t.Helper()
	var legacy, nested releases.Snapshot
	if err := json.Unmarshal(legacyRaw, &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if err := json.Unmarshal(nestedRaw, &nested); err != nil {
		t.Fatalf("nested decode: %v", err)
	}
	return legacy, nested
}

func canonJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestCompatOldClientHonoredArmByteIdentical pins that an old client
// (no ?b=) with a valid honored ?userHash= evaluates the byte-identical
// arm as the direct eval.Assign on that hash: the bucket era adds a
// ?b= branch but never moves the honored path.
func TestCompatOldClientHonoredArmByteIdentical(t *testing.T) {
	const honored = "092081140b677b45"
	snap := fakeSnapshot()
	q := query(`userHash=` + honored + `&platform=ios`)
	ctx, err := BuildContext(q, effectiveUserHash(q, "0123456789abcdef"), "US")
	if err != nil {
		t.Fatalf("old-client honored fetch err = %v", err)
	}
	if ctx.UserHash != honored {
		t.Fatalf("old-client UserHash = %q, want honored %q", ctx.UserHash, honored)
	}
	_, variants := EvaluateSnapshot(snap, ctx, "")
	want := eval.Assign(eval.Experiment{
		Seed: "seed-1",
		Variants: []eval.Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}, honored)
	if variants["new_ui"] != want {
		t.Fatalf("old-client arm = %q, want honored-path arm %q", variants["new_ui"], want)
	}
}

// TestCompatOldClientAnonFallbackArmByteIdentical pins the second old-client
// path: no ?b= + malformed ?userHash= falls through to the daily anonID
// (never 400) and evaluates that anonID's arm verbatim.
func TestCompatOldClientAnonFallbackArmByteIdentical(t *testing.T) {
	const anon = "0123456789abcdef"
	snap := fakeSnapshot()
	q := query(`userHash=nothex&platform=ios`)
	ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
	if err != nil {
		t.Fatalf("old-client malformed-hash fetch must not 400, got %v", err)
	}
	if ctx.UserHash != anon {
		t.Fatalf("old-client fallback UserHash = %q, want anonID %q", ctx.UserHash, anon)
	}
	_, variants := EvaluateSnapshot(snap, ctx, "")
	want := eval.Assign(eval.Experiment{
		Seed: "seed-1",
		Variants: []eval.Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}, anon)
	if variants["new_ui"] != want {
		t.Fatalf("old-client fallback arm = %q, want anonID arm %q", variants["new_ui"], want)
	}
}

// TestCompatCountryIgnoredAlongsideBucket pins ?country= accepted-but-ignored
// when ?b= IS present: the server geo still wins verbatim and a garbage
// client country never errors, exactly as in the no-bucket era.
func TestCompatCountryIgnoredAlongsideBucket(t *testing.T) {
	for name, raw := range map[string]string{
		"conflict": `b=42&country=FR`,
		"garbage":  `b=42&country=not-a-country!!!`,
		"lower":    `b=0&country=us`,
		"absent":   `b=9999`,
	} {
		ctx, err := BuildContext(query(raw), effectiveUserHash(query(raw), "0123456789abcdef"), "US")
		if err != nil {
			t.Fatalf("%s: ?country= alongside ?b= must be accepted, got %v", name, err)
		}
		if ctx.Country != "US" {
			t.Errorf("%s: Country = %q, want server geo %q", name, ctx.Country, "US")
		}
	}
	ctx, err := BuildContext(query(`b=42&country=FR`), effectiveUserHash(query(`b=42`), "0123456789abcdef"), "")
	if err != nil {
		t.Fatalf("empty server geo err = %v", err)
	}
	if ctx.Country != "" {
		t.Errorf("empty server geo: Country = %q, want empty (client ignored)", ctx.Country)
	}
	if ctx.UserHash != "42" {
		t.Errorf("bucket must still win alongside ?country=: UserHash = %q, want 42", ctx.UserHash)
	}
}

// TestCompatEtagExactRuleUnchangedInBucketEra pins the frozen 304 rule:
// If-None-Match matches iff it is byte-exactly the stored etag. The rule
// is independent of ?b= (mint/echo never mangles the etag string), so the
// matrix is asserted without any bucket input.
func TestCompatEtagExactRuleUnchangedInBucketEra(t *testing.T) {
	if !EtagMatches("abc123", "abc123") {
		t.Error("exact etag must match")
	}
	for name, inm := range map[string]string{
		"weak-prefix": `W/"abc123"`,
		"quoted":      `"abc123"`,
		"trailing":    "abc123 ",
		"empty":       "",
		"other":       "deadbeef",
	} {
		if EtagMatches(inm, "abc123") {
			t.Errorf("%s: %q must NOT match stored etag (exact-only)", name, inm)
		}
	}
}

// TestCompatLegacyFoldIdenticalUnderBucket pins the snapshot compat fold
// under a bucket-era context: legacy top-level and nested shapes evaluate
// identically when ctx.UserHash carries a canonical bucket number.
func TestCompatLegacyFoldIdenticalUnderBucket(t *testing.T) {
	legacyRaw := []byte(`{"flags":[{"key":"new_ui","type":"bool","default":false,"group":"","rules":[]}],"experiments":[{"id":"exp1","flag":"new_ui","seed":"seed-1","variants":[{"name":"control","weightBps":5000,"values":{"new_ui":false}},{"name":"treatment","weightBps":5000,"values":{"new_ui":true}}],"status":"running"}]}`)
	nestedRaw := []byte(`{"flags":[{"key":"new_ui","type":"bool","default":false,"group":"","rules":[],"experiments":[{"id":"exp1","seed":"seed-1","variants":[{"name":"control","weightBps":5000,"values":{"new_ui":false}},{"name":"treatment","weightBps":5000,"values":{"new_ui":true}}],"status":"running"}]}]}`)

	legacy, nested := decodeSnapshots(t, legacyRaw, nestedRaw)
	ctx, err := BuildContext(query(`b=42`), "ignored-anon", "")
	if err != nil {
		t.Fatalf("BuildContext b=42 err = %v", err)
	}
	lv, lvr := EvaluateSnapshot(legacy, ctx, "")
	nv, nvr := EvaluateSnapshot(nested, ctx, "")
	if canonJSON(map[string]any{"values": lv, "variants": lvr}) != canonJSON(map[string]any{"values": nv, "variants": nvr}) {
		t.Fatalf("legacy/nested fold differs under ?b=42:\nlegacy %v %v\nnested %v %v", lv, lvr, nv, nvr)
	}
	if lvr["new_ui"] == "" {
		t.Fatal("bucket ctx must land in an experiment arm, got anonymous")
	}
}

// TestCompatBucketArmDayIndependent pins that the stable ?b= arm is
// day-independent: the same ?b=42 evaluated with two DIFFERENT fallback
// anonIDs (stand-ins for the day-D and day-D+1 server anonIDs) yields the
// identical UserHash and the identical arm. The bucket path bypasses
// Manager.AnonID daily salt entirely (effectiveUserHash returns the
// canonical number before the fallback is consulted), so the arm is
// day-independent by construction; the anonID-string substitution IS the
// day boundary for this pure path (no time/salt-store manipulation needed).
func TestCompatBucketArmDayIndependent(t *testing.T) {
	const dayD = "day-d-anon-000001"
	const dayD1 = "day-d1-anon-000002"
	snap := fakeSnapshot()
	q := query(`b=42`)
	ctxD, err := BuildContext(q, effectiveUserHash(q, dayD), "US")
	if err != nil {
		t.Fatalf("day-D bucket fetch err = %v", err)
	}
	ctxD1, err := BuildContext(q, effectiveUserHash(q, dayD1), "US")
	if err != nil {
		t.Fatalf("day-D+1 bucket fetch err = %v", err)
	}
	if ctxD.UserHash != "42" {
		t.Fatalf("day-D UserHash = %q, want canonical bucket %q", ctxD.UserHash, "42")
	}
	if ctxD1.UserHash != "42" {
		t.Fatalf("day-D+1 UserHash = %q, want canonical bucket %q", ctxD1.UserHash, "42")
	}
	_, variantsD := EvaluateSnapshot(snap, ctxD, "")
	_, variantsD1 := EvaluateSnapshot(snap, ctxD1, "")
	if variantsD["new_ui"] == "" {
		t.Fatal("day-D bucket ctx must land in an experiment arm, got anonymous")
	}
	if variantsD["new_ui"] != variantsD1["new_ui"] {
		t.Fatalf("bucket arm moved across day boundary: day-D %q vs day-D+1 %q", variantsD["new_ui"], variantsD1["new_ui"])
	}
}
