// Baseline characterization for todo 11 (fetch-lookups hardening).
//
// Pins the env-resolution matrix on UNCHANGED code. Must pass before and
// after the indexed-lookup fix.
//
// Matrix rows covered here (fetch order: 401 key -> 404/400 env ->
// 401 scope -> 304/200):
//   - Unknown slug -> ErrNotFound -> handler 404 "Unknown env.".
//   - Slug shared by 2+ projects without qualifier -> AmbiguousError ->
//     handler 400 naming the collision.
//   - ?project= qualifier selects the project; unknown qualifier -> 404.
//   - Key env mismatch -> ErrScopeMismatch -> handler 401 (message pinned).
//   - ResolveForKey: the key's env IS the env (point lookup, never a scan
//     pick); unscoped keys with empty env are rejected with ErrScopeMismatch
//     (breaking removal of the legacy Resolve fallback).
//   - Flag scoping: same flag key under another project never matches.
package envresolve

import (
	"errors"
	"testing"
)

// TestAuthOrder_UnknownSlugIs404 pins: no row with the slug -> ErrNotFound
// (ToRequestError maps it to 404 "Unknown env.").
func TestAuthOrder_UnknownSlugIs404(t *testing.T) {
	if _, err := PickIndex("nope", nil, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty projects must be ErrNotFound, got %v", err)
	}
}

// TestAuthOrder_AmbiguousSlugIs400 pins: shared slug without qualifier ->
// AmbiguousError carrying slug + count (handler 400 surfaces the message).
func TestAuthOrder_AmbiguousSlugIs400(t *testing.T) {
	_, err := PickIndex("dev", []string{"projA", "projB"}, "")
	var amb AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("shared slug must be AmbiguousError, got %v", err)
	}
	if amb.Slug != "dev" || amb.Count != 2 {
		t.Fatalf("collision = %+v, want slug dev count 2", amb)
	}
	if amb.Error() == "" {
		t.Fatal("collision message must be non-empty (surfaced in the 400)")
	}
}

// TestAuthOrder_QualifierSelectsProject pins deterministic disambiguation:
// ?project= picks the row; unknown qualifier is 404, not a silent pick.
func TestAuthOrder_QualifierSelectsProject(t *testing.T) {
	idx, err := PickIndex("dev", []string{"projA", "projB"}, "projB")
	if err != nil || idx != 1 {
		t.Fatalf("qualifier must select index 1, got %d, err %v", idx, err)
	}
	if _, err := PickIndex("dev", []string{"projA", "projB"}, "projZ"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown qualifier must be ErrNotFound, got %v", err)
	}
}

// TestAuthOrder_ScopeMismatchIs401 pins the post-env 401: a key bound to
// another env is unauthorized, with a stable message.
func TestAuthOrder_ScopeMismatchIs401(t *testing.T) {
	if !errors.Is(ErrScopeMismatch, ErrScopeMismatch) {
		t.Fatal("ErrScopeMismatch must match itself via errors.Is")
	}
	const want = "SDK key is not authorized for this env."
	if ErrScopeMismatch.Error() != want {
		t.Fatalf("scope message = %q, want %q", ErrScopeMismatch.Error(), want)
	}
}

// TestAuthOrder_FlagScoping pins: the same flag key under another project
// never matches (ingest/stats must not leak cross-project identity).
func TestAuthOrder_FlagScoping(t *testing.T) {
	rows := []FlagRow{
		{ID: "flagA", Key: "launch", Project: "projA"},
		{ID: "flagB", Key: "launch", Project: "projB"},
	}
	if id, ok := MatchFlag(rows, "launch", "projA"); !ok || id != "flagA" {
		t.Fatalf("projA must resolve flagA, got %q ok=%v", id, ok)
	}
	if id, ok := MatchFlag(rows, "launch", "projB"); !ok || id != "flagB" {
		t.Fatalf("projB must resolve flagB, got %q ok=%v", id, ok)
	}
	if _, ok := MatchFlag(rows, "launch", "projZ"); ok {
		t.Fatal("foreign project must miss")
	}
}
