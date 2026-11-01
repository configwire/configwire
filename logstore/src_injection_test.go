package logstore

import (
	"testing"
)

// TestQuerySrcInjectionSafety pins that a malicious src filter cannot
// inject filter syntax: bound params keep it an equality match, so every
// hostile value matches nothing (0 rows, no error) instead of leaking
// rows. The src path is intentionally unvalidated in getLogs (unlike the
// token-constrained columns), so this test guards the parameterization
// in LogQuery.conditions/Query directly.
func TestQuerySrcInjectionSafety(t *testing.T) {
	app := logsTestApp(t)
	seedLogRows(t, app)
	t.Cleanup(func() { resetForTest(Defaults()) })

	// Sanity: the fixture holds 3 ingest + 1 fetch rows before probing.
	if recs, err := Query(app, LogQuery{Level: -1}, 1, 50); err != nil || len(recs) != 4 {
		t.Fatalf("fixture = %d rows, err %v; want 4 rows", len(recs), err)
	}

	malicious := []string{
		`a" && true || x=="`,
		`*`,
		`../../`,
		`" || "1"="1`,
		`ingest" || "a"="a`,
		`%`,
		`_`,
		`ingest && true`,
		`(src="ingest")`,
		`'; DROP TABLE server_logs;--`,
		`ingest || 1==1`,
		`%25`,
	}

	for _, src := range malicious {
		recs, err := Query(app, LogQuery{Level: -1, Src: src}, 1, 50)
		if err != nil {
			t.Fatalf("Query(Src=%q) error = %v, want nil (no error leak)", src, err)
		}
		if len(recs) != 0 {
			t.Fatalf("Query(Src=%q) = %d rows, want 0 (must not leak)", src, len(recs))
		}
		if total := countLogs(t, app, LogQuery{Level: -1, Src: src}); total != 0 {
			t.Fatalf("CountRecords(Src=%q) = %d, want 0 (page/total parity)", src, total)
		}
	}

	// Legitimate src values keep exact-matching after the hostile probes.
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"ingest", 3},
		{"fetch", 1},
	} {
		recs, err := Query(app, LogQuery{Level: -1, Src: tc.src}, 1, 50)
		if err != nil {
			t.Fatalf("Query(Src=%q): %v", tc.src, err)
		}
		if len(recs) != tc.want {
			t.Fatalf("Query(Src=%q) = %d rows, want %d", tc.src, len(recs), tc.want)
		}
	}

	// The table survives: no probe dropped rows or widened the store.
	if recs, err := Query(app, LogQuery{Level: -1}, 1, 50); err != nil || len(recs) != 4 {
		t.Fatalf("post-probe fixture = %d rows, err %v; want 4 rows", len(recs), err)
	}
}
