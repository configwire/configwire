package envresolve

// FlagRow is a DB-free projection so (key, project) matching is unit-testable on fake rows.
type FlagRow struct {
	ID      string
	Key     string
	Project string
}

// MatchFlag resolves a flag key within one project only. Same key under another
// project never matches: stats/ingest must not leak one project's flag
// identity into another's counts. First match wins (flag keys are not
// unique per project; the write path only enforces shape + cap).
func MatchFlag(rows []FlagRow, key, project string) (string, bool) {
	if key == "" || project == "" {
		return "", false
	}
	for _, r := range rows {
		if r.Key == key && r.Project == project {
			return r.ID, true
		}
	}
	return "", false
}
