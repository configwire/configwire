package ingest

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
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

func validRps(v int) bool { return v == 0 || (v >= 1 && v <= 10000) }

// mintInputError validates mint parameters, returning an HTTP status
// and message (ok=true when valid). Missing/unknown env → 404;
// out-of-range budgets → 400.
func mintInputError(app core.App, env string, fetchRps, ingestRps int) (status int, msg string, ok bool) {
	if env == "" {
		return http.StatusNotFound, "Unknown env.", false
	}
	if _, err := app.FindRecordById("environments", env); err != nil {
		return http.StatusNotFound, "Unknown env.", false
	}
	if !validRps(fetchRps) || !validRps(ingestRps) {
		return http.StatusBadRequest, "fetchRps/ingestRps must be empty or 1..10000.", false
	}
	return 0, "", true
}

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
	_ = json.NewDecoder(re.Request.Body).Decode(&req)
	if status, msg, ok := mintInputError(re.App, req.Env, req.FetchRps, req.IngestRps); !ok {
		if status == http.StatusNotFound {
			return re.JSON(http.StatusNotFound, map[string]any{"message": msg, "status": 404})
		}
		return re.BadRequestError(msg, nil)
	}
	rec, full, err := mintKey(re.App, req.Env, req.FetchRps, req.IngestRps)
	if err != nil {
		return re.JSON(http.StatusInternalServerError, map[string]any{"message": "Failed to generate key.", "status": 500})
	}
	log.Printf("configwire: minted sdk key id=%s prefix=%s env=%s", rec.Id, KeyPrefix(full), req.Env)
	return re.JSON(http.StatusCreated, map[string]any{
		"id":     rec.Id,
		"prefix": KeyPrefix(full),
		"key":    full,
	})
}
