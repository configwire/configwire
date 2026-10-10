package eval

import (
	"strconv"
	"strings"
	"testing"
)

func resetRegexCacheForTest() {
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()
	regexCache = map[string]regexCacheEntry{}
	regexCompiles = 0
}

func regexCompileCount() int64 {
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()
	return regexCompiles
}

// TestMatchRegexCachesCompiles pins the fetch hot-path fix: a repeated
// pattern compiles exactly once no matter how many evaluations use it.
func TestMatchRegexCachesCompiles(t *testing.T) {
	resetRegexCacheForTest()
	for i := 0; i < 10; i++ {
		if !matchRegex("android-14", "^and") {
			t.Fatal("valid pattern should match")
		}
	}
	if n := regexCompileCount(); n != 1 {
		t.Fatalf("10 matches of one pattern compiled %d times, want 1", n)
	}
}

// TestMatchRegexCachesFailures pins that uncompilable patterns are
// cached too: every fetch with a bad stored pattern must not pay a
// fresh compile.
func TestMatchRegexCachesFailures(t *testing.T) {
	resetRegexCacheForTest()
	for i := 0; i < 5; i++ {
		if matchRegex("android", "([") {
			t.Fatal("invalid pattern must never match")
		}
	}
	if n := regexCompileCount(); n != 1 {
		t.Fatalf("5 matches of one bad pattern compiled %d times, want 1", n)
	}
}

// TestValidateRegexPatternRejects pins write-time rejection: patterns
// that cannot compile or exceed the length cap are errors (400 at rule
// save / publish), never silent fetch-time misses.
func TestValidateRegexPatternRejects(t *testing.T) {
	if err := ValidateRegexPattern("(["); err == nil {
		t.Fatal("uncompilable pattern should be rejected")
	}
	if err := ValidateRegexPattern(strings.Repeat("a", MaxRegexPatternLength+1)); err == nil {
		t.Fatalf("pattern over %d bytes should be rejected", MaxRegexPatternLength)
	}
	if err := ValidateRegexPattern(strings.Repeat("a", MaxRegexPatternLength)); err != nil {
		t.Fatalf("pattern at exactly the cap should pass: %v", err)
	}
	if err := ValidateRegexPattern("^and"); err != nil {
		t.Fatalf("valid pattern rejected: %v", err)
	}
}

// TestMatchRegexInvalidIsFalse pins fail-closed eval: an invalid or
// oversized pattern is simply false, never a panic or a match.
func TestMatchRegexInvalidIsFalse(t *testing.T) {
	if matchRegex("android", "([") {
		t.Fatal("invalid pattern must never match")
	}
	if matchRegex("android", strings.Repeat("a", MaxRegexPatternLength+1)) {
		t.Fatal("oversized pattern must never match")
	}
}

// TestRegexCacheEvictionBoundsMemory pins that the process-local cache
// cannot grow without bound: past the cap, half the entries are
// evicted and lookups stay correct.
func TestRegexCacheEvictionBoundsMemory(t *testing.T) {
	resetRegexCacheForTest()
	// Distinct patterns (one per i) so the cap is genuinely exceeded
	// and the half-eviction fires.
	for i := 0; i < regexCacheCap+10; i++ {
		matchRegex("t123", "^t"+strconv.Itoa(i)+"$")
	}
	regexCacheMu.Lock()
	n := len(regexCache)
	regexCacheMu.Unlock()
	if n > regexCacheCap {
		t.Fatalf("cache holds %d entries, want at most %d", n, regexCacheCap)
	}
	if !matchRegex("android-14", "^and") {
		t.Fatal("lookup after eviction should still match")
	}
}
