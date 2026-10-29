package ingest

import (
	"log"
	"net/http"
	"strconv"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/internal/apireq"
	"github.com/configwire/configwire/limits"
)

// mintKeyRequest is the admin key-minting body. Only env/fetchRps/
// ingestRps are honored; any client-supplied key material (hash,
// verifier, prefix, key) is ignored — the server generates all of it
// so entropy is guaranteed.
type mintKeyRequest struct {
	Env       string `json:"env"`
	FetchRps  int    `json:"fetchRps"`
	IngestRps int    `json:"ingestRps"`
}

// validRps accepts 0 (inherit the global budget) or 1..field-ceiling,
// where the ceiling comes from the plan env (CONFIGWIRE_MAX_FETCH_RPS /
// CONFIGWIRE_MAX_INGEST_RPS via limits, defaulting to 10000 = no
// ceiling). Unknown fields have no ceiling beyond the absolute bound.
func validRps(field string, v int) bool {
	if v == 0 {
		return true
	}
	if v < 1 || v > 10000 {
		return false
	}
	return v <= fieldCeiling(field)
}

// fieldCeiling maps a per-key budget field to its plan ceiling.
func fieldCeiling(field string) int {
	switch field {
	case limits.FetchRpsField:
		return limits.CeilingFetchRps()
	case limits.IngestRpsField:
		return limits.CeilingIngestRps()
	default:
		return 10000
	}
}

// mintInputError validates mint parameters, returning an HTTP status
// and message (ok=true when valid). Missing/unknown env → 404;
// out-of-range budgets → 400; above-ceiling budgets → 400 naming the
// ceiling.
func mintInputError(app core.App, env string, fetchRps, ingestRps int) (status int, msg string, ok bool) {
	if env == "" {
		return http.StatusNotFound, "Unknown env.", false
	}
	if _, err := app.FindRecordById("environments", env); err != nil {
		return http.StatusNotFound, "Unknown env.", false
	}
	if !validAbsoluteRps(fetchRps) || !validAbsoluteRps(ingestRps) {
		return http.StatusBadRequest, "fetchRps/ingestRps must be empty or 1..10000.", false
	}
	if !validRps(limits.FetchRpsField, fetchRps) {
		return http.StatusBadRequest, "invalid fetchRps: must be 1.." + strconv.Itoa(fieldCeiling(limits.FetchRpsField)) + " (plan ceiling).", false
	}
	if !validRps(limits.IngestRpsField, ingestRps) {
		return http.StatusBadRequest, "invalid ingestRps: must be 1.." + strconv.Itoa(fieldCeiling(limits.IngestRpsField)) + " (plan ceiling).", false
	}
	return 0, "", true
}

// validAbsoluteRps accepts 0 (inherit the global budget) or the absolute
// 1..10000 range, ignoring plan ceilings (the ceiling check owns its own
// message).
func validAbsoluteRps(v int) bool { return v == 0 || (v >= 1 && v <= 10000) }

// mintKey creates a v2 sdk_keys row with server-generated material.
// The DB row stores ONLY the bcrypt verifier (hash=""); the full key
// is returned once for the 201 response and never logged.
func mintKey(app core.App, env string, fetchRps, ingestRps int) (*core.Record, string, error) {
	full, err := GenerateKey()
	if err != nil {
		return nil, "", err
	}
	verifier, err := GenerateVerifier(full)
	if err != nil {
		return nil, "", err
	}
	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		return nil, "", err
	}
	rec := core.NewRecord(col)
	rec.Set("prefix", KeyPrefix(full))
	rec.Set("hash", "")
	rec.Set("keyVer", 2)
	rec.Set("verifier", verifier)
	rec.Set("env", env)
	if fetchRps != 0 {
		rec.Set("fetchRps", fetchRps)
	}
	if ingestRps != 0 {
		rec.Set("ingestRps", ingestRps)
	}
	if err := app.Save(rec); err != nil {
		return nil, "", err
	}
	return rec, full, nil
}

// RegisterKeys mounts the superuser-only key-minting endpoint.
func RegisterKeys(se *core.ServeEvent) {
	se.Router.POST("/api/v1/admin/keys", postMintKey).Bind(apis.RequireSuperuserAuth())
}

func postMintKey(re *core.RequestEvent) error {
	var req mintKeyRequest
	if err := apireq.DecodeBody(re, &req); err != nil {
		return err
	}
	if status, msg, ok := mintInputError(re.App, req.Env, req.FetchRps, req.IngestRps); !ok {
		if status == http.StatusNotFound {
			return re.JSON(http.StatusNotFound, map[string]any{"message": msg, "status": 404})
		}
		return re.BadRequestError(msg, nil)
	}
	rec, full, err := mintKey(re.App, req.Env, req.FetchRps, req.IngestRps)
	if err != nil {
		return apireq.Internal(re, "ingest: failed to mint key", err)
	}
	log.Printf("configwire: minted sdk key id=%s prefix=%s env=%s", rec.Id, KeyPrefix(full), req.Env)
	return re.JSON(http.StatusCreated, map[string]any{
		"id":     rec.Id,
		"prefix": KeyPrefix(full),
		"key":    full,
	})
}
