package ingest

// Audit trail wiring: minting an SDK key emits one key.mint row with the
// env/prefix target and numeric detail — never the full key.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/configwire/configwire/internal/testhelp"
)

func callPostMintKey(t *testing.T, app core.App, body string) (int, map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postMintKey(re); err != nil {
		code, msg := testhelp.CodeMessage(err, rec)
		return code, nil, msg
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode mint body: %v", err)
	}
	return rec.Code, decoded, ""
}

func TestAuditKeyMint(t *testing.T) {
	app := keyVerTestApp(t)
	testhelp.EnsureAuditCollection(t, app)
	env := seedEnv(t, app)

	code, body, msg := callPostMintKey(t, app, `{"env":"`+env+`","fetchRps":100,"ingestRps":50}`)
	if code != http.StatusCreated {
		t.Fatalf("mint = %d %q, want 201", code, msg)
	}
	fullKey, _ := body["key"].(string)
	if !strings.HasPrefix(fullKey, "cw-") {
		t.Fatalf("minted key = %q, want cw- prefix", fullKey)
	}

	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": "key.mint"})
	if err != nil {
		t.Fatalf("find audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("key.mint rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if got := r.GetInt("status"); got != http.StatusCreated {
		t.Errorf("status = %d, want 201", got)
	}
	if got := r.GetString("targetType"); got != "sdk_key" {
		t.Errorf("targetType = %q, want sdk_key", got)
	}
	if got := r.GetString("targetLabel"); got != KeyPrefix(fullKey) {
		t.Errorf("targetLabel = %q, want prefix %q", got, KeyPrefix(fullKey))
	}
	if got := r.GetString("targetId"); got == "" {
		t.Error("targetId is empty")
	}

	raw, err := json.Marshal(r.GetRaw("detail"))
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	detail := string(raw)
	for _, forbidden := range []string{fullKey, "password", "token", "verifier"} {
		if forbidden != "" && strings.Contains(detail, forbidden) {
			t.Errorf("audit detail leaked %q: %s", forbidden, detail)
		}
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if d["env"] != env || d["fetchRps"] != float64(100) || d["ingestRps"] != float64(50) {
		t.Errorf("detail = %v", d)
	}
}
