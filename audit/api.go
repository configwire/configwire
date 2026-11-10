package audit

// Superuser-only audit query endpoint:
//
//	GET /api/v1/admin/audit?action=&actor=&targetType=&targetId=&status=&since=&page=&perPage=
//	    -> {items:[{id,actor,action,targetType,targetId,targetLabel,status,detail,created}],
//	        page,perPage,total}
//
// since uses the "<N>d" grammar (1..1825, default 30); "all" (or an
// explicit 0) removes the time bound. page defaults to 1, perPage to 50
// clamped to 1..200. created is RFC3339; detail is the raw JSON object
// (or null). Mounted in main.go after logstore.Register.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"

	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/security"
)

// defaultSinceDays bounds a query by one month unless asked otherwise.
const defaultSinceDays = 30

// RegisterAPI mounts the superuser-only audit endpoint.
func RegisterAPI(se *core.ServeEvent) {
	se.Router.GET("/api/v1/admin/audit", getAudit).Bind(apis.RequireSuperuserAuth())
}

// parseSince parses the "<N>d" query grammar: empty -> 30, "all"/"0"
// -> 0 (unbounded), "<N>d" with N in 1..1825 -> N. Anything else is
// rejected.
func parseSince(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSinceDays, true
	}
	if strings.EqualFold(raw, "all") || raw == "0" {
		return 0, true
	}
	if !strings.HasSuffix(raw, "d") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
	if err != nil || n < MinDays || n > MaxDays {
		return 0, false
	}
	return n, true
}

// parsePositive parses a positive int query value with a default on empty.
func parsePositive(raw string, def int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// detailValue renders the stored JSON detail as a raw JSON object, or nil
// when the field is empty (null in the response).
func detailValue(r *core.Record) any {
	switch v := r.GetRaw("detail").(type) {
	case types.JSONRaw:
		if len(v) > 0 && strings.TrimSpace(string(v)) != "null" {
			return json.RawMessage(v)
		}
	case []byte:
		if len(v) > 0 && strings.TrimSpace(string(v)) != "null" {
			return json.RawMessage(v)
		}
	case string:
		if s := strings.TrimSpace(v); s != "" && s != "null" {
			return json.RawMessage(s)
		}
	}
	return nil
}

// getAudit handles GET /api/v1/admin/audit (superuser-only).
func getAudit(re *core.RequestEvent) error {
	security.SetHeaders(re)
	q := re.Request.URL.Query()

	sinceDays, ok := parseSince(q.Get("since"))
	if !ok {
		return re.BadRequestError("invalid since: must be <N>d (1..1825) or all.", nil)
	}
	page, ok := parsePositive(q.Get("page"), 1)
	if !ok {
		return re.BadRequestError("invalid page: must be a positive integer.", nil)
	}
	perPage, ok := parsePositive(q.Get("perPage"), defaultPerPage)
	if !ok {
		return re.BadRequestError("invalid perPage: must be a positive integer.", nil)
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	status := 0
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return re.BadRequestError("invalid status: must be all digits.", nil)
		}
		status = n
	}

	f := Filter{
		Action:     strings.TrimSpace(q.Get("action")),
		Actor:      strings.TrimSpace(q.Get("actor")),
		TargetType: strings.TrimSpace(q.Get("targetType")),
		TargetID:   strings.TrimSpace(q.Get("targetId")),
		Status:     status,
		SinceDays:  sinceDays,
		Page:       page,
		PerPage:    perPage,
	}
	recs, total, err := Query(re.App, f)
	if err != nil {
		return apireq.Internal(re, "audit: failed to query audit log", err)
	}
	items := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		items = append(items, map[string]any{
			"id":          r.Id,
			"actor":       r.GetString("actor"),
			"action":      r.GetString("action"),
			"targetType":  r.GetString("targetType"),
			"targetId":    r.GetString("targetId"),
			"targetLabel": r.GetString("targetLabel"),
			"status":      r.GetInt("status"),
			"detail":      detailValue(r),
			"created":     r.GetDateTime("created").Time().UTC().Format(time.RFC3339),
		})
	}
	return re.JSON(http.StatusOK, map[string]any{
		"items":   items,
		"page":    page,
		"perPage": perPage,
		"total":   total,
	})
}
