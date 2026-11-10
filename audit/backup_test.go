package audit

// Backup-route audit: the native /api/backups routes have no request context in
// their core hooks, so a router middleware maps method+path to an action and
// records only successful operations.

import (
	"net/http"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/internal/testhelp"
)

func TestBackupActionMapping(t *testing.T) {
	cases := []struct {
		method, path, action, name string
		ok                         bool
	}{
		{http.MethodPost, "/api/backups", "backup.create", "", true},
		{http.MethodPost, "/api/backups/", "backup.create", "", true},
		{http.MethodGet, "/api/backups", "", "", false},
		{http.MethodPost, "/api/backups/upload", "backup.upload", "", true},
		{http.MethodDelete, "/api/backups/pb-2026.tar.gz", "backup.delete", "pb-2026.tar.gz", true},
		{http.MethodGet, "/api/backups/pb%202026.tar.gz", "backup.download", "pb 2026.tar.gz", true},
		{http.MethodPost, "/api/backups/pb-2026/restore", "backup.restore", "pb-2026", true},
		{http.MethodGet, "/api/backups/pb-2026/restore", "", "", false},
		{http.MethodPut, "/api/backups/pb-2026", "", "", false},
		{http.MethodGet, "/api/v1/admin/env/qa/stats", "", "", false},
	}
	for _, c := range cases {
		action, name, ok := backupAction(c.method, c.path)
		if ok != c.ok || action != c.action || name != c.name {
			t.Errorf("backupAction(%s %s) = (%q, %q, %v), want (%q, %q, %v)",
				c.method, c.path, action, name, ok, c.action, c.name, c.ok)
		}
	}
}

func TestRecordBackupEvent(t *testing.T) {
	app := newAuditTestApp(t)
	admin := testhelp.MustSuperuser(t, app, "admin@example.com")

	re := &core.RequestEvent{App: app, Auth: admin}
	recordBackupEvent(re, "backup.create", "pb-1.zip", "/api/backups", http.StatusOK)

	rows, _, err := Query(app, Filter{Action: "backup.create"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("backup.create rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if got := r.GetString("actor"); got != "admin@example.com" {
		t.Errorf("actor = %q", got)
	}
	if got := r.GetString("targetType"); got != "backup" {
		t.Errorf("targetType = %q", got)
	}
	if got := r.GetString("targetLabel"); got != "pb-1.zip" {
		t.Errorf("targetLabel = %q", got)
	}
	if got := r.GetInt("status"); got != http.StatusOK {
		t.Errorf("status = %d", got)
	}
}
