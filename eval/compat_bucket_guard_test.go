package eval

// T8 backward-compat regression guard: the eval layer never validates
// bucket strings — every string, canonical ("42") or non-canonical
// ("007", "-1", "10000"), flows through the same Bucket/Assign formula
// deterministically. The "0"-"9999" canonical rule and its 400 live in
// fetch.ParseBucket (server) and ingest.parseBucketCanonical (events)
// only. Additive; no existing assertion touched.
//
// Already pinned elsewhere (referenced, not duplicated):
//   - Bucket/Assign formula incl. 10k determinism/distribution: vectors suite
//   - sequential-input uniformity + cross-seed decorrelation gate: TestBucketSequentialUniformity,
//     TestBucketCrossSeedDecorrelation, TestBucketUniformityGateRedControl
//   - Bucket(number+seed) arm on the fetch side: TestBucketNumberSeedEvaluation (fetch pkg)

import "testing"

// TestCompatEvalNeverValidatesBucketStrings pins the layering: eval
// computes for any input string (no error concept here) and repeats
// deterministically, so old honored hashes and new bucket numbers share
// one code path with no bucket special-case.
func TestCompatEvalNeverValidatesBucketStrings(t *testing.T) {
	exp := Experiment{
		Seed: "seed-1",
		Variants: []Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}
	for _, s := range []string{"42", "0", "9999", "007", "-1", "10000", "092081140b677b45"} {
		first, second := Assign(exp, s), Assign(exp, s)
		if first != second {
			t.Fatalf("Assign(%q) nondeterministic: %q vs %q", s, first, second)
		}
		if b := Bucket(s, exp.Seed); b < 0 || b > 9999 {
			t.Fatalf("Bucket(%q) = %d, want 0-9999", s, b)
		}
	}
	if Bucket("", exp.Seed) != -1 {
		t.Fatal("anonymous fallthrough moved: Bucket(\"\") must stay -1")
	}
	if got := Assign(exp, ""); got != exp.DefaultVariant {
		t.Fatalf("anonymous Assign = %q, want DefaultVariant %q", got, exp.DefaultVariant)
	}
	if Bucket("42", "seed-1") != Bucket("42", "seed-1") {
		t.Fatal("Bucket must be a pure function of its inputs")
	}
}
