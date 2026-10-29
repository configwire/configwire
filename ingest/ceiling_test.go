package ingest

import (
	"strings"
	"testing"

	"github.com/configwire/configwire/limits"
)

func clearRpsCeilingEnvs(t *testing.T) {
	t.Helper()
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "")
}

func TestValidRpsNoCeilingPreservesShape(t *testing.T) {
	clearRpsCeilingEnvs(t)
	if !validRps(limits.FetchRpsField, 0) || !validRps(limits.IngestRpsField, 0) {
		t.Fatal("0 (global) must be valid without ceiling")
	}
	if !validRps(limits.FetchRpsField, 1) || !validRps(limits.IngestRpsField, 10000) {
		t.Fatal("1..10000 must be valid without ceiling")
	}
	if validRps(limits.FetchRpsField, -1) || validRps(limits.IngestRpsField, 10001) {
		t.Fatal("out-of-range must be invalid without ceiling")
	}
}

func TestValidRpsCeilingRejects(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "10")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "5")
	if !validRps(limits.FetchRpsField, 10) || !validRps(limits.IngestRpsField, 5) {
		t.Fatal("at-ceiling values must be valid")
	}
	if validRps(limits.FetchRpsField, 11) {
		t.Fatal("fetchRps 11 above ceiling 10 must be invalid")
	}
	if validRps(limits.IngestRpsField, 6) {
		t.Fatal("ingestRps 6 above ceiling 5 must be invalid")
	}
	if !validRps(limits.FetchRpsField, 0) {
		t.Fatal("0 (global) must stay valid under a ceiling")
	}
}

func TestMintInputErrorCeilingRejects(t *testing.T) {
	t.Setenv("CONFIGWIRE_MAX_FETCH_RPS", "10")
	t.Setenv("CONFIGWIRE_MAX_INGEST_RPS", "5")
	app := keyVerTestApp(t)
	env := seedEnv(t, app)
	// At-ceiling mints pass; above-ceiling mints fail 400 naming the ceiling.
	if _, _, ok := mintInputError(app, env, 10, 5); !ok {
		t.Fatal("at-ceiling budgets must be valid")
	}
	for _, tc := range []struct {
		name       string
		fetchRps   int
		ingestRps  int
		wantSubstr string
	}{
		{"fetch", 11, 5, "fetchRps"},
		{"ingest", 10, 6, "ingestRps"},
	} {
		st, msg, ok := mintInputError(app, env, tc.fetchRps, tc.ingestRps)
		if ok || st != 400 {
			t.Fatalf("%s above ceiling must be 400, got status=%d ok=%v", tc.name, st, ok)
		}
		if !strings.Contains(msg, "(plan ceiling)") || !strings.Contains(msg, tc.wantSubstr) {
			t.Fatalf("%s message should name the ceiling, got %q", tc.name, msg)
		}
	}
	// Absolute violations keep the legacy shape even under a ceiling.
	if st, msg, ok := mintInputError(app, env, 0, 10001); ok || st != 400 ||
		msg != "fetchRps/ingestRps must be empty or 1..10000." {
		t.Fatalf("absolute violation must keep legacy message, got status=%d msg=%q ok=%v", st, msg, ok)
	}
}
