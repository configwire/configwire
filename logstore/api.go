package logstore

import (
	"net/http"
	"strconv"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/security"
)

// Register loads persisted tunables (seeding the singleton row when
// absent) and mounts the superuser-only admin endpoints:
//
//	GET /api/v1/admin/log-settings -> {enabled,maxDays,successLogs}
//	PUT /api/v1/admin/log-settings -> validates maxDays 1..1825,
//	    updates memory + upserts the log_settings row,
//	    returns the same shape.
//	    Each field is optional: absent (or null) keeps the stored value
//	    for back-compat; explicit values replace it.
//	GET /api/v1/admin/logs?level=&src=&project=&env=&key=&status=&reason=&limiter=&page=&perPage= ->
//	    {items:[{id,level,message,src,env,created}],page,perPage,total}.
//	    level/src/project/env/key/status/reason/limiter are optional filters;
//	    page defaults to 1, perPage defaults to 50 clamped to 1..200.
func Register(se *core.ServeEvent) {
	EnsureLoaded(se.App)
	se.Router.GET("/api/v1/admin/log-settings", getLogSettings).Bind(apis.RequireSuperuserAuth())
	se.Router.PUT("/api/v1/admin/log-settings", putLogSettings).Bind(apis.RequireSuperuserAuth())
	se.Router.GET("/api/v1/admin/logs", getLogs).Bind(apis.RequireSuperuserAuth())
}

// logSettingsBody is the wire shape for GET responses and PUT requests.
// Pointers keep absent/null (keep stored) distinct from explicit values.
type logSettingsBody struct {
	Enabled     *bool `json:"enabled"`
	MaxDays     *int  `json:"maxDays"`
	SuccessLogs *bool `json:"successLogs"`
}

func toSettingsBody(c Settings) map[string]any {
	return map[string]any{
		"enabled":     c.Enabled,
		"maxDays":     c.MaxDays,
		"successLogs": c.SuccessLogs,
	}
}

// getLogSettings handles GET /api/v1/admin/log-settings (superuser-only).
func getLogSettings(re *core.RequestEvent) error {
	security.SetHeaders(re)
	return re.JSON(http.StatusOK, toSettingsBody(GetConfig()))
}

// putLogSettings handles PUT /api/v1/admin/log-settings (superuser-only).
// Order: 400 (body/validation) -> 500 (persist failure) -> 200 (same
// shape). Absent/null fields keep the stored values; explicit values
// replace them for ValidateSettings to accept or reject.
func putLogSettings(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req logSettingsBody
	if err := apireq.DecodeBody(re, &req); err != nil {
		return err
	}
	stored := GetConfig()
	next := Settings{
		Enabled:     resolvePUTBool(stored.Enabled, req.Enabled),
		MaxDays:     resolvePUTInt(stored.MaxDays, req.MaxDays),
		SuccessLogs: resolvePUTBool(stored.SuccessLogs, req.SuccessLogs),
	}
	if err := ValidateSettings(next); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	if err := UpdateSettings(re.App, next); err != nil {
		return apireq.Internal(re, "logstore: failed to persist log settings", err)
	}
	return re.JSON(http.StatusOK, toSettingsBody(next))
}

// resolvePUTBool maps one PUT knob onto the stored value: nil
// (absent/null) keeps stored, otherwise the explicit value is returned.
func resolvePUTBool(stored bool, req *bool) bool {
	if req == nil {
		return stored
	}
	return *req
}

// resolvePUTInt maps one PUT knob onto the stored value: nil
// (absent/null) keeps stored, otherwise the explicit value is returned
// for ValidateSettings to accept or reject.
func resolvePUTInt(stored int, req *int) int {
	if req == nil {
		return stored
	}
	return *req
}

// validLogToken reports whether s is safe to match as a message k=v
// token value: [A-Za-z0-9_-] only, so LIKE metacharacters (%, _) and
// spaces (the token separator) cannot appear.
func validLogToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// validLogStatus reports whether s is safe to match as a status filter
// value: all digits, so it parses to an int for the indexed equality
// filter on the status column.
func validLogStatus(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// getLogs handles GET /api/v1/admin/logs (superuser-only). level/src/
// project/env/key/status/reason/limiter are optional filters (absent level
// matches any level, blank src matches any src, blank column filters
// match any row); page defaults to 1, perPage defaults to 50 clamped to
// 1..200. Text filter values are constrained to [A-Za-z0-9_-] and status
// to [0-9], so no filter can inject filter syntax.
func getLogs(re *core.RequestEvent) error {
	security.SetHeaders(re)
	q := re.Request.URL.Query()
	level := -1
	if raw := strings.TrimSpace(q.Get("level")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < minLevel || n > maxLevel {
			return re.BadRequestError("invalid level: must be 0..3.", nil)
		}
		level = n
	}
	src := strings.TrimSpace(q.Get("src"))
	project := strings.TrimSpace(q.Get("project"))
	env := strings.TrimSpace(q.Get("env"))
	key := strings.TrimSpace(q.Get("key"))
	status := strings.TrimSpace(q.Get("status"))
	reason := strings.TrimSpace(q.Get("reason"))
	limiter := strings.TrimSpace(q.Get("limiter"))
	for _, val := range [][2]string{
		{"project", project}, {"env", env}, {"key", key},
		{"reason", reason}, {"limiter", limiter},
	} {
		if val[1] != "" && !validLogToken(val[1]) {
			return re.BadRequestError("invalid "+val[0]+": must be [A-Za-z0-9_-].", nil)
		}
	}
	if status != "" && !validLogStatus(status) {
		return re.BadRequestError("invalid status: must be [0-9].", nil)
	}
	lq := LogQuery{
		Level: level, Src: src, Project: project, Env: env, Key: key,
		Status: status, Reason: reason, Limiter: limiter,
	}
	page := 1
	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 {
			page = n
		}
	}
	perPage := 50
	if raw := strings.TrimSpace(q.Get("perPage")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			perPage = n
		}
	}
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 200 {
		perPage = 200
	}
	recs, err := Query(re.App, lq, page, perPage)
	if err != nil {
		return apireq.Internal(re, "logstore: failed to query logs", err)
	}
	var exprs []dbx.Expression
	if conds, params := lq.conditions(); len(conds) > 0 {
		exprs = append(exprs, dbx.NewExp(strings.Join(conds, " AND "), params))
	}
	total, err := re.App.CountRecords(logsCollection, exprs...)
	if err != nil {
		return apireq.Internal(re, "logstore: failed to count logs", err)
	}
	items := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		items = append(items, map[string]any{
			"id":      r.Id,
			"level":   r.GetInt("level"),
			"message": r.GetString("message"),
			"src":     r.GetString("src"),
			"env":     r.GetString("env"),
			"created": r.GetDateTime("created").String(),
		})
	}
	return re.JSON(http.StatusOK, map[string]any{
		"items":   items,
		"page":    page,
		"perPage": perPage,
		"total":   total,
	})
}
