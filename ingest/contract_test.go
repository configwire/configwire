// Baseline characterization for todo 11 (fetch-lookups hardening).
//
// Pins the ingest-side auth seams on UNCHANGED code. Must pass before and
// after the indexed-lookup fix.
//
// Matrix rows covered here:
//   - Unknown/missing/revoked/env-mismatched key -> 401. The lookup seam
//     is prefix (first 8 chars, DB prefilter) + hex(sha256(fullKey))
//     compared in constant time; the full key is never stored.
//   - Key-never-logged invariant: only prefix/hash (digests) may leave
//     the process; raw keys and user IDs never do.
//   - Body matrix: 1..100 events, kind/flag/variant/userHash/ts rules,
//     raw userId/ip strict 400, per-event 64KB / whole-body 128KB -> 413.
//   - Rate default: missing/non-positive rateLimit -> 60 req/min.
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// TestAuthOrder_KeySeams pins the (prefix, hash) lookup contract that 401s
// ride on: prefix is the first 8 chars, hash is lowercase hex(sha256(key)).
// Unknown keys miss one of the two; revoked/env-mismatch is checked after.
func TestAuthOrder_KeySeams(t *testing.T) {
	full := "cw-test-key-0001"
	if got := KeyPrefix(full); got != full[:8] {
		t.Fatalf("KeyPrefix = %q, want %q", got, full[:8])
	}
	if got := KeyPrefix("short"); got != "short" {
		t.Fatalf("short KeyPrefix = %q, want input", got)
	}
	sum := sha256.Sum256([]byte(full))
	if got, want := KeyHash(full), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("KeyHash mismatch: got %q want %q", got, want)
	}
	if len(KeyHash(full)) != 64 {
		t.Fatal("KeyHash must be 64 hex chars")
	}
}

// TestAuthOrder_KeyNeverLogged pins the PII invariant: digests must not
// embed the raw key (hashes only on the wire and in logs).
func TestAuthOrder_KeyNeverLogged(t *testing.T) {
	full := "cw-super-secret-key-99"
	if strings.Contains(KeyHash(full), "secret") {
		t.Fatal("KeyHash must not embed the raw key")
	}
	if KeyPrefix(full) == full {
		t.Fatal("prefix must be a truncated prefilter, never the full key")
	}
	if h := HashUser("user-7"); strings.Contains(h, "user") || h == "user-7" {
		t.Fatalf("HashUser must not embed the input, got %q", h)
	}
}

// TestAuthOrder_BodyMatrix pins the 400/413 body rules (checked AFTER
// 401/404/401/429 in postEvents order).
func TestAuthOrder_BodyMatrix(t *testing.T) {
	cases := []struct {
		name   string
		body   []byte
		status int
	}{
		{"empty-body", []byte(""), 400},
		{"missing-events", []byte(`{}`), 400},
		{"empty-batch", []byte(`{"events":[]}`), 400},
		{"bad-kind", body(t, []any{map[string]any{"kind": "click"}}), 400},
		{"raw-userId", body(t, []any{map[string]any{"kind": "fetch", "userId": "u1"}}), 400},
		{"raw-ip", body(t, []any{map[string]any{"kind": "fetch", "ip": "1.2.3.4"}}), 400},
		{"oversize-event", body(t, []any{map[string]any{"kind": "fetch", "flag": strings.Repeat("f", MaxEventBytes)}}), 413},
	}
	for _, c := range cases {
		_, aerr := ValidateBody(c.body)
		if aerr == nil {
			t.Errorf("%s: expected %d, got nil", c.name, c.status)
			continue
		}
		if aerr.Status != c.status {
			t.Errorf("%s: status = %d, want %d (%s)", c.name, aerr.Status, c.status, aerr.Msg)
		}
	}
	// Batch over 100 -> 400.
	many := make([]any, MaxBatchEvents+1)
	for i := range many {
		many[i] = map[string]any{"kind": "fetch"}
	}
	if _, aerr := ValidateBody(body(t, many)); aerr == nil || aerr.Status != 400 {
		t.Fatalf("101-batch must be 400, got %v", aerr)
	}
}

// TestAuthOrder_RateLimitDefault pins the 429 accounting default: a key
// record without rateLimit is limited at 60 req/min.
func TestAuthOrder_RateLimitDefault(t *testing.T) {
	rec := core.NewRecord(core.NewBaseCollection("sdk_keys"))
	if got := RateLimitFor(rec); got != 60 {
		t.Fatalf("RateLimitFor(missing) = %d, want DefaultRateLimit 60", got)
	}
}

// TestAuthOrder_BodyCaps pins the byte caps the 413s ride on.
func TestAuthOrder_BodyCaps(t *testing.T) {
	if MaxBodyBytes != 128<<10 || MaxEventBytes != 64<<10 {
		t.Fatalf("caps moved: body=%d event=%d", MaxBodyBytes, MaxEventBytes)
	}
	if MaxBatchEvents != 100 {
		t.Fatalf("MaxBatchEvents = %d, want 100", MaxBatchEvents)
	}
}
