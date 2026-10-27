package sso

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// callRedeemFromIP invokes postRedeem with an explicit client IP,
// reporting status + Retry-After header + raw body. re.JSON errors are
// nil on write (the existing callRedeem convention), so the recorder
// carries the status for every path including the 429.
func callRedeemFromIP(app core.App, srv *server, ip, body string) (int, string, string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sso/redeem", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.RemoteAddr = ip + ":5678"
	}
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	_ = srv.postRedeem(re)
	return rec.Code, rec.Header().Get("Retry-After"), rec.Body.String()
}

// mintValidRedeem builds a fresh single-use ticket for email (distinct
// nonce per call so sequential redeems never collide on replay).
func mintValidRedeem(t *testing.T, email string, secret []byte) string {
	t.Helper()
	nonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	return MintTicket(email, time.Now().Unix()+60, nonce, secret)
}

// TestRedeemPerIPBucket pins the per-route bucket: the first
// RedeemPerIPLimit redeems from one IP pass (200 with fresh tickets),
// the next within the window is rejected with 429 + Retry-After: 1 and
// the exact global-limiter 429 body.
func TestRedeemPerIPBucket(t *testing.T) {
	app := ssoTestApp(t)
	createTestSuperuser(t, app, "owner@example.com")
	secret := testSecret(t)
	srv := testServer(ModeSSO, secret)
	const ip = "203.0.113.7"

	for i := 0; i < RedeemPerIPLimit; i++ {
		ticket := mintValidRedeem(t, "owner@example.com", secret)
		code, _, body := callRedeemFromIP(app, srv, ip, `{"ticket":`+quoteJSON(ticket)+`}`)
		if code != http.StatusOK {
			t.Fatalf("redeem %d/%d: status=%d body=%s, want 200", i+1, RedeemPerIPLimit, code, redactLen(body))
		}
	}

	ticket := mintValidRedeem(t, "owner@example.com", secret)
	code, retryAfter, body := callRedeemFromIP(app, srv, ip, `{"ticket":`+quoteJSON(ticket)+`}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("over-budget redeem: status=%d body=%s, want 429", code, redactLen(body))
	}
	if retryAfter != "1" {
		t.Fatalf("over-budget redeem: Retry-After=%q, want %q", retryAfter, "1")
	}
	var got struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("over-budget redeem body=%s: %v", redactLen(body), err)
	}
	if got.Message != "Rate limit exceeded." || got.Status != http.StatusTooManyRequests {
		t.Fatalf("over-budget redeem body=%q, want global-limiter 429 shape", body)
	}
	if strings.Contains(body, ticket) {
		t.Fatalf("429 body echoes the ticket: %s", redactLen(body))
	}
}

// TestRedeemPerIPBucketsIndependent pins per-IP isolation: exhausting
// one IP's bucket leaves another IP's legit redeem on the 200 path.
func TestRedeemPerIPBucketsIndependent(t *testing.T) {
	app := ssoTestApp(t)
	createTestSuperuser(t, app, "owner@example.com")
	secret := testSecret(t)
	srv := testServer(ModeSSO, secret)

	for i := 0; i < RedeemPerIPLimit; i++ {
		code, _, _ := callRedeemFromIP(app, srv, "203.0.113.7", `{"ticket":"bogus"}`)
		if code != http.StatusBadRequest {
			t.Fatalf("filler %d: status=%d, want 400 (invalid attempts still consume budget)", i+1, code)
		}
	}
	code, _, _ := callRedeemFromIP(app, srv, "203.0.113.7", `{"ticket":"bogus"}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("exhausted IP: status=%d, want 429", code)
	}

	ticket := mintValidRedeem(t, "owner@example.com", secret)
	code, _, body := callRedeemFromIP(app, srv, "198.51.100.9", `{"ticket":`+quoteJSON(ticket)+`}`)
	if code != http.StatusOK {
		t.Fatalf("other IP redeem: status=%d body=%s, want 200", code, redactLen(body))
	}
}
