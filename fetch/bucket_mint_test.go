package fetch

import (
	"strconv"
	"testing"

	"github.com/configwire/configwire/identity"
)

// T2 failing-first: cold 200 without ?b= mints a canonical 0-9999 bucket.
func TestResponseBucketMintedWhenAbsent(t *testing.T) {
	got, err := EffectiveBucket(query(``))
	if err != nil {
		t.Fatalf("absent ?b=: EffectiveBucket err = %v", err)
	}
	n, cerr := strconv.Atoi(got)
	if cerr != nil || n < 0 || n > 9999 {
		t.Fatalf("absent ?b=: minted bucket = %q, want canonical 0-9999", got)
	}
	if !identity.IsCanonicalBucket(got) {
		t.Fatalf("absent ?b=: minted bucket = %q, want canonical form (no padding)", got)
	}
}

// T2 failing-first: ?b=42 echoes/uses 42 (no mint override).
func TestResponseBucketEchoesClientBucket(t *testing.T) {
	for _, b := range []string{"42", "0", "9999", "7"} {
		got, err := EffectiveBucket(query(`b=` + b))
		if err != nil {
			t.Fatalf("b=%s: EffectiveBucket err = %v", b, err)
		}
		if got != b {
			t.Fatalf("b=%s: EffectiveBucket = %q, want echo %q (no mint override)", b, got, b)
		}
	}
}

// T2 failing-first: mint is uniform-capable stdlib crypto/rand, canonical.
func TestMintBucketRangeCanonical(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		got, err := MintBucket()
		if err != nil {
			t.Fatalf("MintBucket err = %v", err)
		}
		if !identity.IsCanonicalBucket(got) {
			t.Fatalf("MintBucket = %q, want canonical 0-9999", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("MintBucket minted only %d distinct values in 200 draws, want randomness (>1)", len(seen))
	}
}

// T2 failing-first: malformed ?b= still surfaces the single 400 rule here.
func TestResponseBucketMalformedIs400(t *testing.T) {
	if _, err := EffectiveBucket(query(`b=007`)); err == nil {
		t.Fatal("b=007: expected 400-mapped error, got nil")
	} else if err.Error() != wantBucketErr {
		t.Fatalf("b=007: error = %q, want %q", err.Error(), wantBucketErr)
	}
}

// T2 failing-first: evaluated 200 carries the effective bucket field.
func TestEvaluatedBodyCarriesBucket(t *testing.T) {
	body := evaluatedBody(1, "abc123", map[string]any{"k": true}, map[string]string{}, "42", "2026-01-01T00:00:00Z")
	got, ok := body["bucket"]
	if !ok {
		t.Fatal("evaluated 200: missing bucket field")
	}
	if got != "42" {
		t.Fatalf("evaluated 200: bucket = %v, want effective %q", got, "42")
	}
}

// T2 failing-first: empty release omits bucket; 304 path never mints.
func TestEmptyReleaseBodyOmitsBucket(t *testing.T) {
	body := emptyReleaseBody("2026-01-01T00:00:00Z")
	if _, ok := body["bucket"]; ok {
		t.Fatalf("empty release: must omit bucket, got %v", body["bucket"])
	}
	if body["version"] != 0 || body["etag"] != "none" {
		t.Fatalf("empty release shape moved: %+v", body)
	}
	vals, ok := body["values"].(map[string]any)
	if !ok || len(vals) != 0 {
		t.Fatalf("empty release values must be {}, got %v", body["values"])
	}
}
