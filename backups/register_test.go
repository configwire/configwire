package backups

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
)

// TestBackupApiRegisteredAndSuperuserGated is a smoke test pinning that
// PocketBase's backup API stays registered and superuser-gated in the
// ConfigWire app.
//
// Upstream wiring (PocketBase v0.40.4 apis/backup.go bindBackupApi):
// GET "" list + POST "" create + POST /upload + GET /{key} download +
// DELETE /{key} + POST /{key}/restore under /api/backups, all except the
// file-token download bound with RequireSuperuserAuth().
//
// The unauthenticated-list assertion is the load-bearing one: it expects
// 401, so a missing/unregistered route (404) fails the test, while a
// registered-but-open route would fail with 200. The superuser-list
// assertion pins the happy path still answers a 200 JSON array.
//
// no_ui safety: the backup API is NOT gated on ui.DistDirFS (unlike the
// /_/ admin UI; see the GO_TAGS block in configwire/Dockerfile — upstream
// ui/embed_no_ui.go leaves DistDirFS nil so only /_/ routes drop). All
// /api/* routes are unaffected, so `-tags no_ui` builds keep backups
// working; this test exercises exactly that router shape.
//
// Hermeticity: scenarios run against an in-memory httptest recorder (no
// real server boot, no fixed ports) on TestApp clones in temp dirs (no
// fixture files, no repo residue). The scenario path never calls Resolve,
// but CONFIGWIRE_BACKUP_* are neutralized anyway so ambient env cannot
// leak in.
func TestBackupApiRegisteredAndSuperuserGated(t *testing.T) {
	t.Setenv(CronEnvVar, "")
	t.Setenv(KeepEnvVar, "")

	scenarios := []tests.ApiScenario{
		{
			Name:           "unauthenticated backup list is rejected, not missing",
			Method:         http.MethodGet,
			URL:            "/api/backups",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedContent: []string{
				`"data":{}`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "superuser backup list answers a JSON array",
			Method: http.MethodGet,
			URL:    "/api/backups",
			Headers: map[string]string{
				// Test superuser token for the default tests.TestApp data
				// (same credential used by upstream apis/backup_test.go).
				"Authorization": "eyJhbGciOiJIUzI1NiJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoicGJjXzMxNDI2MzU4MjMiLCJleHAiOjI1MjQ2MDQ0NjEsInJlZnJlc2hhYmxlIjp0cnVlfQ.UXgO3j-0BumcugrFjbd7j0M4MQvbrLggLlcu_YNGjoY",
			},
			ExpectedStatus: http.StatusOK,
			ExpectedContent: []string{
				`[]`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
