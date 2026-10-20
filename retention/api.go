package retention

import (
	"net/http"

	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/security"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Register loads persisted tunables (seeding the singleton row when
// absent) and mounts the superuser-only admin endpoints:
//
//	GET /api/v1/admin/retention -> {rawDays,rollupDays}
//	PUT /api/v1/admin/retention -> validates 1..1825 + rollup>=raw,
//	    updates memory + upserts the retention_settings row,
//	    returns the same shape.
//	    Each field is optional: absent (or null) keeps the stored value
//	    for back-compat; explicit values replace it.
func Register(se *core.ServeEvent) {
	ensureLoaded(se.App)
	se.Router.GET("/api/v1/admin/retention", getRetention).Bind(apis.RequireSuperuserAuth())
	se.Router.PUT("/api/v1/admin/retention", putRetention).Bind(apis.RequireSuperuserAuth())
}

// retentionBody is the wire shape for GET responses and PUT requests.
// Pointers keep absent/null (keep stored) distinct from explicit values.
type retentionBody struct {
	RawDays    *int `json:"rawDays"`
	RollupDays *int `json:"rollupDays"`
}

func toBody(c Config) map[string]any {
	return map[string]any{
		"rawDays":    c.RawDays,
		"rollupDays": c.RollupDays,
	}
}

// getRetention handles GET /api/v1/admin/retention (superuser-only).
func getRetention(re *core.RequestEvent) error {
	security.SetHeaders(re)
	return re.JSON(http.StatusOK, toBody(GetConfig()))
}

// putRetention handles PUT /api/v1/admin/retention (superuser-only). Order:
// 400 (body/validation) -> 500 (persist failure) -> 200 (same shape).
// Absent/null fields keep the stored values; explicit values replace them
// for ValidateConfig to accept or reject.
func putRetention(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req retentionBody
	if err := apireq.DecodeBody(re, &req); err != nil {
		return err
	}
	stored := GetConfig()
	next := Config{
		RawDays:    resolvePUTDays(stored.RawDays, req.RawDays),
		RollupDays: resolvePUTDays(stored.RollupDays, req.RollupDays),
	}
	if err := ValidateConfig(next); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	if err := upsertGlobalRow(re.App, next); err != nil {
		return apireq.Internal(re, "retention: failed to persist retention settings", err)
	}
	if err := applyConfig(next); err != nil {
		return apireq.Internal(re, "retention: failed to apply retention settings", err)
	}
	return re.JSON(http.StatusOK, toBody(next))
}

// resolvePUTDays maps one PUT knob onto the stored value: nil
// (absent/null) keeps stored, otherwise the explicit value is returned
// for ValidateConfig to accept or reject.
func resolvePUTDays(stored int, req *int) int {
	if req == nil {
		return stored
	}
	return *req
}
