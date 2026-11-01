package main

// Opt-in admin-request audit trail: the log-settings admin-log area
// controls what lands in server_logs. When the area is enabled, every
// admin API request is recorded as one info line ("METHOD /path ->
// status", reason admin-request); successLines off keeps only non-2xx
// lines, healthChecks on additionally records /healthz (reason
// health-check, src health). Registered after the security/allowlist
// BindFunc, so denied IPs are already rejected (and logged by the
// limiter) before this runs. Best-effort: a store failure never fails
// the request. The master log-store switch still gates everything
// (Write no-ops when the store is off). Path only — query strings can
// carry secrets (backup file tokens, SSO tickets) and are never logged.
// Tokens, bodies, and IPs are never logged either.

import (
	"net/http"
	"strconv"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/configwire/configwire/limits"
	"github.com/configwire/configwire/logstore"
)

// statusWriter captures the response status for the audit line. Status
// defaults to 200 when the handler writes a body without an explicit
// WriteHeader (net/http implicit-200 semantics).
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush passes through when the underlying writer supports streaming,
// so wrapping never breaks a streaming response.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// adminAuditEntry builds one server_logs line: "METHOD /path ->
// status" at info level. The path never carries a query string (callers
// pass URL.Path only).
func adminAuditEntry(src, method, path string, status int, reason string) logstore.Entry {
	return logstore.Entry{
		Level:   1, // info
		Src:     src,
		Message: method + " " + path + " -> " + strconv.Itoa(status),
		Status:  status,
		Reason:  reason,
	}
}

// logAdminRequests records one audit line per admin API request while
// the admin-log area is enabled, plus /healthz while healthChecks is
// on. Anything else costs two prefix checks plus one RWMutex snapshot
// and passes through untouched.
func logAdminRequests(re *core.RequestEvent) error {
	if re == nil || re.Request == nil || re.Request.URL == nil {
		return re.Next()
	}
	path := re.Request.URL.Path
	cfg := logstore.GetConfig().AdminLogs
	isHealth := path == "/healthz"
	switch {
	case isHealth:
		if !cfg.Enabled || !cfg.HealthChecks {
			return re.Next()
		}
	case limits.IsAdminPath(path):
		if !cfg.Enabled {
			return re.Next()
		}
	default:
		return re.Next()
	}
	sw := &statusWriter{ResponseWriter: re.Response}
	re.Response = sw
	err := re.Next()
	status := sw.status
	if status == 0 {
		status = http.StatusOK
	}
	if !cfg.SuccessLines && status >= 200 && status <= 299 {
		return err
	}
	src, reason := "admin", "admin-request"
	if isHealth {
		src, reason = "health", "health-check"
	}
	_ = logstore.Write(re.App, adminAuditEntry(src, re.Request.Method, path, status, reason))
	return err
}
