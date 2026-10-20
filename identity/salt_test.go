package identity

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRotationAtUTCMidnight: salts change exactly at the UTC midnight
// boundary; the same UTC day reuses one salt (cache + store agree).
func TestRotationAtUTCMidnight(t *testing.T) {
	m := NewManager(NewMemoryStore())
	just := time.Date(2026, 5, 1, 23, 59, 59, 0, time.UTC)
	mid := time.Date(2026, 5, 2, 0, 0, 1, 0, time.UTC)

	a, err := m.SaltForDay("env-a", just)
	if err != nil {
		t.Fatal(err)
	}
	aAgain, err := m.SaltForDay("env-a", just)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(aAgain) {
		t.Fatal("same UTC day returned different salts")
	}
	b, err := m.SaltForDay("env-a", mid)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(b) {
		t.Fatal("no rotation: pre/post UTC midnight share one salt")
	}
	if DayString(just) != "2026-05-01" || DayString(mid) != "2026-05-02" {
		t.Fatalf("DayString not UTC YYYY-MM-DD: %q %q", DayString(just), DayString(mid))
	}
}

// TestPruneKeepsPreviousDay48h: salts newer than 48h survive (including
// yesterday), older ones are deleted.
func TestPruneKeepsPreviousDay48h(t *testing.T) {
	st := NewMemoryStore()
	m := NewManager(st)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	for _, d := range []string{"2026-05-06", "2026-05-07", "2026-05-08", "2026-05-09", "2026-05-10"} {
		tm, _ := time.Parse("2006-01-02", d)
		if _, err := m.SaltForDay("env-a", tm); err != nil {
			t.Fatal(err)
		}
	}
	n, err := m.Prune("env-a", now)
	if err != nil {
		t.Fatal(err)
	}
	// cutoff = DayString(now-48h) = 2026-05-08: days < it (06, 07) go.
	if n != 2 {
		t.Fatalf("Prune deleted %d rows, want 2", n)
	}
	for _, d := range []string{"2026-05-08", "2026-05-09", "2026-05-10"} {
		if _, ok, _ := st.Load("env-a", d); !ok {
			t.Fatalf("day %s pruned but must be kept (<=48h)", d)
		}
	}
	for _, d := range []string{"2026-05-06", "2026-05-07"} {
		if _, ok, _ := st.Load("env-a", d); ok {
			t.Fatalf("day %s kept but must be deleted (>48h)", d)
		}
	}
}

// TestUniqueEnvDay: the (envID, day) pair is unique — a second Save for
// the same pair reports ErrSaltExists instead of silently forking.
func TestUniqueEnvDay(t *testing.T) {
	st := NewMemoryStore()
	salt := make([]byte, SaltLen)
	if err := st.Save("env-a", "2026-05-10", salt); err != nil {
		t.Fatal(err)
	}
	if err := st.Save("env-a", "2026-05-10", salt); err != ErrSaltExists {
		t.Fatalf("second Save = %v, want ErrSaltExists", err)
	}
	// Same day, different env is a distinct pair.
	if err := st.Save("env-b", "2026-05-10", salt); err != nil {
		t.Fatal(err)
	}
}

// TestConcurrentSameDay: concurrent GetOrCreate for one (env, day)
// converges on a single salt (mutex, no fork).
func TestConcurrentSameDay(t *testing.T) {
	m := NewManager(NewMemoryStore())
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	const n = 16
	out := make([]string, n)
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := m.SaltForDay("env-a", now)
			if err != nil {
				t.Error(err)
				return
			}
			out[i] = string(s)
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		if out[i] != out[0] {
			t.Fatal("concurrent SaltForDay forked two salts for one (env, day)")
		}
	}
}

// TestSaltLength32: generated salts are 32 bytes from crypto/rand.
func TestSaltLength32(t *testing.T) {
	s, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != SaltLen {
		t.Fatalf("salt length = %d, want %d", len(s), SaltLen)
	}
}

// TestRestartStabilitySameStore: a rebuilt Manager over the same Store
// (the restart shape — process-local cache is gone, durable salts are
// not) derives the same anonID for the same envID/day/ip/ua, so
// same-day bucketing survives restarts once the Store is durable.
func TestRestartStabilitySameStore(t *testing.T) {
	st := NewMemoryStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	ip, ua := "203.0.113.7", "ConfigWire/1.0"

	before, err := NewManager(st).AnonID("env-a", ip, ua, now)
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewManager(st).AnonID("env-a", ip, ua, now)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("restart changed same-day anonID: %q != %q", before, after)
	}
}

// testOnlyImportsOf returns the import paths of src via go/parser for
// the stdlib-only proof test.
func testOnlyImportsOf(t *testing.T, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}
func testOnlyReadPackageSources(t *testing.T, includeTests bool) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".go" {
			continue
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(raw)
	}
	if len(out) == 0 {
		t.Fatal("no package sources found")
	}
	return out
}
