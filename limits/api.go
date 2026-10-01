package limits

import (
	"bytes"
	"io"
	"net/http"

	"github.com/configwire/configwire/security"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Register loads persisted tunables (seeding the singleton row when
// absent) and mounts the superuser-only admin endpoints:
//
//	GET /api/v1/admin/limits -> {globalRps,burst,fetchRps,ingestRps,adminRps,windowSec:1}
//	PUT /api/v1/admin/limits -> validates 1..10000, updates memory + upserts
//	    the rate_settings row, returns the same shape.
func Register(se *core.ServeEvent) {
	ensureLoaded(se.App)
	se.Router.GET("/api/v1/admin/limits", getLimits).Bind(apis.RequireSuperuserAuth())
	se.Router.PUT("/api/v1/admin/limits", putLimits).Bind(apis.RequireSuperuserAuth())
}

// limitsBody is the wire shape for GET responses and PUT requests.
type limitsBody struct {
	GlobalRps int `json:"globalRps"`
	Burst     int `json:"burst"`
	FetchRps  int `json:"fetchRps"`
	IngestRps int `json:"ingestRps"`
	AdminRps  int `json:"adminRps"`
}

func toBody(c Config) map[string]any {
	return map[string]any{
		"globalRps": c.GlobalRps,
		"burst":     c.Burst,
		"fetchRps":  c.FetchRps,
		"ingestRps": c.IngestRps,
		"adminRps":  c.AdminRps,
		"windowSec": WindowSec,
	}
}

// decodeBody mirrors account.decodeBody: empty bodies -> 400 (BindBody
// alone would silently return nil); malformed JSON -> 400.
func decodeBody(re *core.RequestEvent, dst any) error {
	if re.Request.ContentLength == 0 {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	if re.Request.Body == nil {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(re.Request.Body, (1<<20)+1))
	if err != nil {
		return re.BadRequestError("malformed JSON body: "+err.Error(), nil)
	}
	// Restore for BindBody.
	re.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(bytes.TrimSpace(raw)) == 0 {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	if err := re.BindBody(dst); err != nil {
		return re.BadRequestError("malformed JSON body: "+err.Error(), nil)
	}
	return nil
}

// getLimits handles GET /api/v1/admin/limits (superuser-only).
func getLimits(re *core.RequestEvent) error {
	security.SetHeaders(re)
	return re.JSON(http.StatusOK, toBody(GetConfig()))
}

// putLimits handles PUT /api/v1/admin/limits (superuser-only). Order:
// 400 (body/validation) -> 500 (persist failure) -> 200 (same shape).
func putLimits(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req limitsBody
	if err := decodeBody(re, &req); err != nil {
		return err
	}
	next := Config{
		GlobalRps: req.GlobalRps,
		Burst:     req.Burst,
		FetchRps:  req.FetchRps,
		IngestRps: req.IngestRps,
		AdminRps:  req.AdminRps,
	}
	if err := ValidateConfig(next); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	if err := upsertGlobalRow(re.App, next); err != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "failed to persist rate settings: " + err.Error(),
			"status":  http.StatusInternalServerError,
		})
	}
	if err := applyConfig(next); err != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{
			"message": "failed to apply rate settings: " + err.Error(),
			"status":  http.StatusInternalServerError,
		})
	}
	return re.JSON(http.StatusOK, toBody(next))
}

// upsertGlobalRow writes the singleton rate_settings row (key=global),
// creating it when absent. Uses the request-scoped app (never captured).
func upsertGlobalRow(app core.App, c Config) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}
	rec, err := findGlobalRow(app)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("key", globalKey)
	}
	rec.Set("rps", c.GlobalRps)
	rec.Set("burst", c.Burst)
	rec.Set("fetchRps", c.FetchRps)
	rec.Set("ingestRps", c.IngestRps)
	rec.Set("adminRps", c.AdminRps)
	return app.Save(rec)
}
