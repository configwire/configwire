package audit

// HTTP backup operations use PocketBase's native /api/backups routes, whose
// core hooks carry no request or auth context. This middleware wraps only
// those routes, so a successful backup create/upload/download/delete/restore
// lands in the trail with the acting superuser. The download route is
// authenticated by a file token, not re.Auth, so it records actor "system";
// scheduled (cron) backups never pass through a request and stay unaudited.

import (
	"net/http"
	"net/url"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// RegisterBackupAudit binds the backup-route middleware. Called from main's
// OnServe next to RegisterAPI.
func RegisterBackupAudit(se *core.ServeEvent) {
	se.Router.BindFunc(backupAudit)
}

// backupAction maps one method+path to an audit action and the backup key.
// The list endpoint and non-backup paths return ok=false (untouched).
func backupAction(method, path string) (action, name string, ok bool) {
	p := strings.TrimSuffix(path, "/")
	switch {
	case p == "/api/backups":
		if method == http.MethodPost {
			return "backup.create", "", true
		}
	case p == "/api/backups/upload":
		if method == http.MethodPost {
			return "backup.upload", "", true
		}
	case strings.HasPrefix(p, "/api/backups/"):
		parts := strings.Split(strings.TrimPrefix(p, "/api/backups/"), "/")
		key := parts[0]
		if unescaped, err := url.PathUnescape(key); err == nil {
			key = unescaped
		}
		if len(parts) == 1 {
			switch method {
			case http.MethodDelete:
				return "backup.delete", key, true
			case http.MethodGet:
				return "backup.download", key, true
			}
		}
		if len(parts) == 2 && parts[1] == "restore" && method == http.MethodPost {
			return "backup.restore", key, true
		}
	}
	return "", "", false
}

// backupStatusWriter captures the response status so only successful
// operations are audited (a rejected or 404 request is not an action).
type backupStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *backupStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *backupStatusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *backupStatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func backupAudit(re *core.RequestEvent) error {
	if re == nil || re.Request == nil || re.Request.URL == nil {
		return re.Next()
	}
	action, name, ok := backupAction(re.Request.Method, re.Request.URL.Path)
	if !ok {
		return re.Next()
	}
	sw := &backupStatusWriter{ResponseWriter: re.Response}
	re.Response = sw
	err := re.Next()
	status := sw.status
	if status == 0 {
		status = http.StatusOK
	}
	if status < 200 || status > 299 {
		return err
	}
	recordBackupEvent(re, action, name, re.Request.URL.Path, status)
	return err
}

// recordBackupEvent writes one backup row. TargetID and targetLabel both carry
// the backup key (empty for a create whose generated name the response did not
// expose); Detail keeps the route only, never a file token or query string.
func recordBackupEvent(re *core.RequestEvent, action, name, path string, status int) {
	email, id := ActorFromRequest(re)
	RecordApp(re.App, Entry{
		Actor:       email,
		ActorID:     id,
		Action:      action,
		TargetType:  "backup",
		TargetID:    name,
		TargetLabel: name,
		Status:      status,
		Detail:      map[string]any{"path": path},
	})
}
