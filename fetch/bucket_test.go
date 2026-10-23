package fetch

import (
	"strings"
	"testing"

	"github.com/configwire/configwire/eval"
)

const wantBucketErr = `invalid "b": send "0"-"9999".`

const wantUidErr = `raw "uid" is forbidden: send "userHash" instead.`

func TestParseBucketCanonicalHonored(t *testing.T) {
	for _, raw := range []string{`b=0`, `b=9999`, `b=42`, `b=1`} {
		q := query(raw)
		got, err := ParseBucket(q)
		if err != nil {
			t.Errorf("%s: expected valid bucket, got err %v", raw, err)
			continue
		}
		want := strings.TrimPrefix(raw, "b=")
		if got != want {
			t.Errorf("%s: ParseBucket = %q, want %q", raw, got, want)
		}
	}
}

func TestParseBucketMalformedIs400(t *testing.T) {
	for _, raw := range []string{
		`b=abc`,
		`b=-1`,
		`b=10000`,
		`b=007`,
		`b=00`,
		`b=01`,
		`b=12.0`,
		`b=+12+`,
		`b=+12`,
		`b=12+`,
		`b=`,
		`b=+++`,
		`b=1e3`,
		`b=0x2A`,
		`b=+42+`,
	} {
		// b= (empty) is absent, not malformed — skip it here.
		if raw == `b=` {
			continue
		}
		q := query(raw)
		if got, err := ParseBucket(q); err == nil {
			t.Errorf("%s: expected 400-mapped error, got bucket %q", raw, got)
		} else if err.Error() != wantBucketErr {
			t.Errorf("%s: error = %q, want %q", raw, err.Error(), wantBucketErr)
		}
	}
}

func TestParseBucketAbsent(t *testing.T) {
	for _, raw := range []string{``, `platform=ios`, `userHash=092081140b677b45`} {
		q := query(raw)
		got, err := ParseBucket(q)
		if err != nil {
			t.Errorf("%s: absent ?b= must not error, got %v", raw, err)
			continue
		}
		if got != "" {
			t.Errorf("%s: absent ?b= ParseBucket = %q, want empty", raw, got)
		}
	}
}

func TestBucketPrecedenceWinsOverUserHash(t *testing.T) {
	const anon = "0123456789abcdef"
	q := query(`b=42&userHash=092081140b677b45`)
	if got := effectiveUserHash(q, anon); got != "42" {
		t.Fatalf("effectiveUserHash with ?b=42 = %q, want bucket %q", got, "42")
	}
	ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
	if err != nil {
		t.Fatalf("BuildContext err = %v", err)
	}
	if ctx.UserHash != "42" {
		t.Fatalf("ctx.UserHash = %q, want ?b= arm %q (?b= wins)", ctx.UserHash, "42")
	}
	// Canonical edges win too.
	for _, b := range []string{"0", "9999"} {
		q := query(`b=` + b + `&userHash=092081140b677b45`)
		ctx, err := BuildContext(q, effectiveUserHash(q, anon), "")
		if err != nil {
			t.Fatalf("b=%s: err = %v", b, err)
		}
		if ctx.UserHash != b {
			t.Errorf("b=%s: UserHash = %q, want bucket", b, ctx.UserHash)
		}
	}
}

func TestBucketAbsentIgnoresUserHashFallsBackToAnon(t *testing.T) {
	const anon = "0123456789abcdef"
	// Absent ?b= + valid ?userHash= -> ignored, server anonID wins.
	q := query(`userHash=092081140b677b45&platform=ios`)
	ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
	if err != nil {
		t.Fatalf("ignored err = %v", err)
	}
	if ctx.UserHash != anon {
		t.Fatalf("absent ?b= + valid hash: UserHash = %q, want server anonID %q (userHash ignored)", ctx.UserHash, anon)
	}
	// Absent ?b= + malformed ?userHash= -> anonID fallback, never 400.
	q = query(`userHash=nothex&platform=ios`)
	ctx, err = BuildContext(q, effectiveUserHash(q, anon), "US")
	if err != nil {
		t.Fatalf("malformed hash must not error, got %v", err)
	}
	if ctx.UserHash != anon {
		t.Fatalf("malformed fallback UserHash = %q, want anonID %q", ctx.UserHash, anon)
	}
	// Different ?userHash= values yield the SAME bucketing (server anonID).
	for _, raw := range []string{
		`userHash=092081140b677b45`,
		`userHash=ffffffffffffffff`,
		`userHash=nothex`,
		`platform=ios`,
	} {
		q := query(raw)
		ctx, err := BuildContext(q, effectiveUserHash(q, anon), "US")
		if err != nil {
			t.Fatalf("%s: err = %v", raw, err)
		}
		if ctx.UserHash != anon {
			t.Fatalf("%s: UserHash = %q, want server anonID %q", raw, ctx.UserHash, anon)
		}
	}
}

func TestBucketMalformedIs400ViaBuildContext(t *testing.T) {
	const anon = "0123456789abcdef"
	for _, b := range []string{"abc", "-1", "10000", "007", "12.0", " 12 "} {
		q := query(`platform=ios&userHash=092081140b677b45`)
		q.Set("b", b)
		if _, err := BuildContext(q, effectiveUserHash(q, anon), ""); err == nil {
			t.Errorf("b=%q: expected 400-mapped error, got nil", b)
		} else if err.Error() != wantBucketErr {
			t.Errorf("b=%q: error = %q, want %q", b, err.Error(), wantBucketErr)
		}
	}
}

func TestBucketWithUidStill400UidWins(t *testing.T) {
	const anon = "0123456789abcdef"
	for _, raw := range []string{
		`b=42&uid=user-7`,
		`uid=user-7&b=42`,
		`b=42&uid=user-7&userHash=092081140b677b45`,
		`b=abc&uid=user-7`,
	} {
		q := query(raw)
		if _, err := BuildContext(q, effectiveUserHash(q, anon), ""); err == nil {
			t.Errorf("%s: expected 400, got nil", raw)
		} else if err.Error() != wantUidErr {
			t.Errorf("%s: error = %q, want uid-wins %q", raw, err.Error(), wantUidErr)
		}
	}
}

func TestBucketNumberSeedEvaluation(t *testing.T) {
	snap := fakeSnapshot()
	ctx, err := BuildContext(query(`b=42`), "ignored-anon", "")
	if err != nil {
		t.Fatalf("BuildContext err = %v", err)
	}
	if ctx.UserHash != "42" {
		t.Fatalf("ctx.UserHash = %q, want canonical bucket %q", ctx.UserHash, "42")
	}
	values, variants := EvaluateSnapshot(snap, ctx, "")
	wantVariant := eval.Assign(eval.Experiment{
		Seed: "seed-1",
		Variants: []eval.Variant{
			{Name: "control", WeightBps: 5000},
			{Name: "treatment", WeightBps: 5000},
		},
	}, "42")
	if variants["new_ui"] != wantVariant {
		t.Errorf("variant = %q, want Bucket(\"42\"+seed) arm %q", variants["new_ui"], wantVariant)
	}
	wantVal := false
	if wantVariant == "treatment" {
		wantVal = true
	}
	if values["new_ui"] != wantVal {
		t.Errorf("overlay value = %v, want %v for variant %q", values["new_ui"], wantVal, wantVariant)
	}
}
