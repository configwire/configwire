package sso

// Audit trail wiring: a successful ticket redeem emits one sso.redeem row
// with the account target and status, never the ticket.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"github.com/configwire/configwire/internal/testhelp"
)

func TestAuditSSORedeem(t *testing.T) {
	app := ssoTestApp(t)
	testhelp.EnsureAuditCollection(t, app)
	createTestSuperuser(t, app, "owner@example.com")
	secret := testSecret(t)
	srv := testServer(ModeSSO, secret)
	ticket := mintValidRedeem(t, "owner@example.com", secret)

	code, body := callRedeem(app, srv, `{"ticket":`+quoteJSON(ticket)+`}`)
	if code != http.StatusOK {
		t.Fatalf("redeem = %d %s, want 200", code, body)
	}

	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": "sso.redeem"})
	if err != nil {
		t.Fatalf("find audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sso.redeem rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if got := r.GetInt("status"); got != http.StatusOK {
		t.Errorf("status = %d, want 200", got)
	}
	if got := r.GetString("targetLabel"); got != "owner@example.com" {
		t.Errorf("targetLabel = %q, want owner@example.com", got)
	}
	if got := r.GetString("targetType"); got != "account" {
		t.Errorf("targetType = %q, want account", got)
	}
	raw, _ := json.Marshal(r.GetRaw("detail"))
	if strings.Contains(string(raw), ticket) {
		t.Errorf("audit detail leaked the ticket: %s", raw)
	}
}
