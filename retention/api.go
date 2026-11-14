package retention

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/configwire/configwire/audit"
	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/security"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// Register loads persisted tunables (seeding the singleton row when
// absent) and mounts the superuser-only admin endpoints:
//
//	GET /api/v1/admin/retention -> {retentionDays,rawDays,rollupDays}
//	PUT /api/v1/admin/retention -> accepts {retentionDays} (single knob)
//	    or the legacy {rawDays,rollupDays} pair as an advanced override.
//	    Validates 1..1825 + rawDays<=MaxRawDays + rawDays<=rollupDays,
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
	RetentionDays *int `json:"retentionDays"`
	RawDays       *int `json:"rawDays"`
	RollupDays    *int `json:"rollupDays"`
}

func toBody(c Config) map[string]any {
	return map[string]any{
		"retentionDays": c.RollupDays,
		"rawDays":       c.RawDays,
		"rollupDays":    c.RollupDays,
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
//
// The canonical input is {retentionDays: N}. The legacy {rawDays,rollupDays}
// pair is still accepted as an advanced override, but rawDays is capped at
// MaxRawDays and must be <= rollupDays. Mixing retentionDays with
// rawDays/rollupDays is rejected.
func putRetention(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req retentionBody
	if err := apireq.DecodeBody(re, &req); err != nil {
		return err
	}
	stored := GetConfig()
	next, err := resolveRetentionPUT(stored, req)
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	if err := ValidateConfig(next); err != nil {
		if req.RetentionDays != nil {
			err = singleKnobError(err)
		}
		return re.BadRequestError(err.Error(), nil)
	}
	if err := upsertGlobalRow(re.App, next); err != nil {
		return apireq.Internal(re, "retention: failed to persist retention settings", err)
	}
	if err := applyConfig(next); err != nil {
		return apireq.Internal(re, "retention: failed to apply retention settings", err)
	}
	audit.Record(re, audit.Entry{
		Action: "retention.update", TargetType: "settings",
		TargetLabel: "retention", Status: http.StatusOK,
		Detail: map[string]any{
			"retentionDays": next.RollupDays,
			"rawDays":       next.RawDays,
			"rollupDays":    next.RollupDays,
		},
	})
	return re.JSON(http.StatusOK, toBody(next))
}

// resolveRetentionPUT merges the request onto the stored config.
// retentionDays is the single-knob input; rawDays/rollupDays are the
// legacy advanced pair. They must not be mixed.
func resolveRetentionPUT(stored Config, req retentionBody) (Config, error) {
	hasSingle := req.RetentionDays != nil
	hasRaw := req.RawDays != nil
	hasRollup := req.RollupDays != nil
	if hasSingle && (hasRaw || hasRollup) {
		return stored, fmt.Errorf("retentionDays cannot be combined with rawDays/rollupDays; use retentionDays only")
	}
	next := stored
	if hasSingle {
		next.RollupDays = *req.RetentionDays
		next.RawDays = min(next.RollupDays, MaxRawDays)
		return next, nil
	}
	if hasRollup {
		next.RollupDays = *req.RollupDays
	}
	if hasRaw {
		next.RawDays = *req.RawDays
	}
	return next, nil
}

// singleKnobError rewrites a ValidateConfig message for single-knob
// requests: the caller sent retentionDays, never rawDays/rollupDays, so
// naming the derived fields would confuse clients. Either the rawDays or
// the rollupDays range/ceiling check can fire (rawDays is derived as
// min(retentionDays, MaxRawDays), so e.g. retentionDays:0 fails on rawDays
// first) — every rewrite names retentionDays.
func singleKnobError(err error) error {
	ce, ok := err.(*configError)
	if !ok {
		return err
	}
	msg := strings.ReplaceAll(ce.msg, "rollupDays", "retentionDays")
	msg = strings.ReplaceAll(msg, "rawDays", "retentionDays")
	return &configError{msg: msg}
}
