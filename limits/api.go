package limits

import (
	"net/http"

	"github.com/configwire/configwire/identity"
	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/security"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// Register loads persisted tunables (seeding the singleton row when
// absent) and mounts the superuser-only admin endpoints:
//
//	GET /api/v1/admin/limits -> {globalRps,burst,fetchRps,ingestRps,adminAllowedIPs,ipHeaders,countryHeaders,clientIp,country,remoteAddr,windowSec:1}
//	PUT /api/v1/admin/limits -> validates numerics 1..10000 + allowlist +
//	    header orders, updates memory + upserts the rate_settings row,
//	    returns the same shape.
//	    ipHeaders is optional: absent (or null) keeps the stored order for
//	    back-compat; an explicit empty list resets to the code defaults.
//	    countryHeaders is optional with the same semantics (absent/null
//	    keeps stored; explicit empty resets to the code defaults).
//	    adminAllowedIPs is optional: absent (or null) keeps stored;
//	    explicit empty stays empty (allow all).
func Register(se *core.ServeEvent) {
	ensureLoaded(se.App)
	se.Router.GET("/api/v1/admin/limits", getLimits).Bind(apis.RequireSuperuserAuth())
	se.Router.PUT("/api/v1/admin/limits", putLimits).Bind(apis.RequireSuperuserAuth())
}

// limitsBody is the wire shape for GET responses and PUT requests.
// IPHeaders is a pointer so absent/null (keep stored order) stays
// distinct from an explicit empty list (reset to the code defaults).
// CountryHeaders has the same absent/null-vs-empty semantics for the
// country-code header order. AdminAllowedIPs is a pointer so
// absent/null (keep stored) stays distinct from an explicit empty list
// (allow all).
type limitsBody struct {
	GlobalRps       int       `json:"globalRps"`
	Burst           int       `json:"burst"`
	FetchRps        int       `json:"fetchRps"`
	IngestRps       int       `json:"ingestRps"`
	AdminAllowedIPs *[]string `json:"adminAllowedIPs"`
	IPHeaders       *[]string `json:"ipHeaders"`
	CountryHeaders  *[]string `json:"countryHeaders"`
}

func toBody(c Config) map[string]any {
	headers := append([]string(nil), c.IPHeaders...)
	if len(headers) == 0 {
		headers = DefaultIPHeaders()
	}
	country := append([]string(nil), c.CountryHeaders...)
	if len(country) == 0 {
		country = DefaultCountryHeaders()
	}
	allowed := append([]string(nil), c.AdminAllowedIPs...)
	if allowed == nil {
		allowed = []string{}
	}
	return map[string]any{
		"globalRps":       c.GlobalRps,
		"burst":           c.Burst,
		"fetchRps":        c.FetchRps,
		"ingestRps":       c.IngestRps,
		"adminAllowedIPs": allowed,
		"ipHeaders":       headers,
		"countryHeaders":  country,
		"windowSec":       WindowSec,
	}
}

// toBodyForRequest is toBody plus the resolved client IP for the current
// request (so the UI can display which IP the allowlist sees), the
// resolved ISO-2 country through the active country-header order (so the
// UI can display which geo fetch/ingest derive), and the raw remote
// address host.
func toBodyForRequest(re *core.RequestEvent, c Config) map[string]any {
	body := toBody(c)
	body["clientIp"] = ClientIP(re)
	body["country"] = identity.CountryFromHeadersWith(re.Request.Header, c.CountryHeaders)
	body["remoteAddr"] = remoteHostFromAddr(re.Request.RemoteAddr)
	return body
}

// getLimits handles GET /api/v1/admin/limits (superuser-only).
func getLimits(re *core.RequestEvent) error {
	security.SetHeaders(re)
	return re.JSON(http.StatusOK, toBodyForRequest(re, GetConfig()))
}

// putLimits handles PUT /api/v1/admin/limits (superuser-only). Order:
// 400 (body/validation) -> 500 (persist failure) -> 200 (same shape).
// ipHeaders absent/null keeps the stored order; explicit empty resets to
// the code defaults. countryHeaders has the same absent/null-vs-empty
// semantics. adminAllowedIPs absent/null keeps stored; explicit
// empty stays empty (allow all, never reset to defaults).
func putLimits(re *core.RequestEvent) error {
	security.SetHeaders(re)
	var req limitsBody
	if err := apireq.DecodeBody(re, &req); err != nil {
		return err
	}
	stored := GetConfig()
	next := Config{
		GlobalRps:       req.GlobalRps,
		Burst:           req.Burst,
		FetchRps:        req.FetchRps,
		IngestRps:       req.IngestRps,
		AdminAllowedIPs: resolvePUTAdminIPs(stored.AdminAllowedIPs, req.AdminAllowedIPs),
		IPHeaders:       resolvePUTHeaders(stored.IPHeaders, req.IPHeaders),
		CountryHeaders:  resolvePUTCountryHeaders(stored.CountryHeaders, req.CountryHeaders),
	}
	if err := ValidateConfig(next); err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	if err := upsertGlobalRow(re.App, next); err != nil {
		return apireq.Internal(re, "limits: failed to persist rate settings", err)
	}
	if err := applyConfig(next); err != nil {
		return apireq.Internal(re, "limits: failed to apply rate settings", err)
	}
	return re.JSON(http.StatusOK, toBodyForRequest(re, next))
}

// resolvePUTAdminIPs maps the PUT adminAllowedIPs field onto the stored
// allowlist: nil (absent/null) keeps stored, otherwise the normalized
// list is returned (explicit empty stays empty = allow all) for
// ValidateConfig to accept or reject.
func resolvePUTAdminIPs(stored []string, req *[]string) []string {
	if req == nil {
		return append([]string(nil), stored...)
	}
	if len(*req) == 0 {
		return []string{}
	}
	return normalizeAdminAllowedIPs(*req)
}

// resolvePUTHeaders maps the PUT ipHeaders field onto the stored order:
// nil (absent/null) keeps the stored order, an empty (or all-blank)
// list resets to the code defaults, otherwise the normalized order is
// returned for ValidateConfig to accept or reject.
func resolvePUTHeaders(stored []string, req *[]string) []string {
	if req == nil {
		return append([]string(nil), stored...)
	}
	if norm := normalizeIPHeaders(*req); len(norm) > 0 {
		return norm
	}
	return DefaultIPHeaders()
}

// resolvePUTCountryHeaders maps the PUT countryHeaders field onto the
// stored order with the same nil-vs-empty semantics as
// resolvePUTHeaders: nil (absent/null) keeps the stored order, an empty
// (or all-blank) list resets to the code defaults, otherwise the
// normalized order is returned for ValidateConfig to accept or reject.
func resolvePUTCountryHeaders(stored []string, req *[]string) []string {
	if req == nil {
		return append([]string(nil), stored...)
	}
	if norm := normalizeCountryHeaders(*req); len(norm) > 0 {
		return norm
	}
	return DefaultCountryHeaders()
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
	setAdminAllowedIPsField(collection, rec, c.AdminAllowedIPs)
	setIPHeadersField(collection, rec, c.IPHeaders)
	setCountryHeadersField(collection, rec, c.CountryHeaders)
	return app.Save(rec)
}
