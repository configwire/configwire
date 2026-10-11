package sso

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/configwire/configwire/internal/testhelp"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// testSecretHex is the fixed P0 fixture secret (test-only).
const testSecretHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testSecret(t *testing.T) []byte {
	t.Helper()
	b, err := ParseSecretHex(testSecretHex)
	if err != nil {
		t.Fatalf("ParseSecretHex(fixture): %v", err)
	}
	return b
}

type ssoVector struct {
	Name      string `json:"name"`
	SecretHex string `json:"secret_hex"`
	Email     string `json:"email"`
	Expiry    int64  `json:"expiry"`
	Nonce     string `json:"nonce"`
	Ticket    string `json:"ticket"`
	Valid     bool   `json:"valid"`
	Reason    string `json:"reason"`
}

// loadVectors reads the normative P0 fixtures at repo-root docs/.
func loadVectors(t *testing.T) []ssoVector {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "sso_vectors.json"))
	if err != nil {
		t.Fatalf("read sso_vectors.json: %v", err)
	}
	var vecs []ssoVector
	if err := json.Unmarshal(raw, &vecs); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}
	if len(vecs) == 0 {
		t.Fatal("no vectors loaded")
	}
	return vecs
}

// TestVectors consumes docs/sso_vectors.json: every well-formed vector's
// MAC is recomputed independently (spec §9 gate), and ValidateTicket
// maps each vector to its specced outcome at a fixed now where the
// valid vector is live (expiry 2000000000) and the expired one is past
// (expiry 1000000000).
func TestVectors(t *testing.T) {
	const nowUnix = 1500000000 // between the expired and valid expiries
	for _, v := range loadVectors(t) {
		secret, err := ParseSecretHex(v.SecretHex)
		if err != nil {
			t.Fatalf("%s: bad fixture secret: %v", v.Name, err)
		}
		// Independent recomputation (spec §9): for 4-segment tickets,
		// recompute the MAC over email/expiry/nonce and compare with the
		// ticket's MAC segment. valid + expired + malformed-b64 carry true
		// MACs (malformed-b64 fails earlier at base64); tampered carries a
		// flipped one.
		if parts := strings.Split(v.Ticket, "."); len(parts) == 4 {
			mac := hmac.New(sha256.New, secret)
			mac.Write([]byte(v.Email + "\n" + strconv.FormatInt(v.Expiry, 10) + "\n" + v.Nonce))
			wantMAC := hex.EncodeToString(mac.Sum(nil))
			if v.Name != "tampered-mac" && wantMAC != parts[3] {
				t.Errorf("%s: MAC recompute mismatch:\n got %s\nwant %s", v.Name, parts[3], wantMAC)
			}
			if v.Name == "tampered-mac" && wantMAC == parts[3] {
				t.Errorf("tampered-mac: recompute unexpectedly matches (fixture not tampered?)")
			}
		}
		pt, err := ValidateTicket(v.Ticket, secret, nowUnix)
		switch v.Name {
		case "valid":
			if err != nil {
				t.Errorf("valid: ValidateTicket: %v", v.Reason)
			} else if pt.Email != v.Email || pt.Nonce != v.Nonce || pt.Expiry != v.Expiry {
				t.Errorf("valid: claims = %+v, want email/nonce/expiry of fixture", pt)
			}
		case "expired":
			if !errors.Is(err, ErrTicketExpired) {
				t.Errorf("expired: err = %v, want ErrTicketExpired", err)
			}
		case "tampered-mac":
			if !errors.Is(err, ErrTicketTampered) {
				t.Errorf("tampered-mac: err = %v, want ErrTicketTampered", err)
			}
		default: // malformed-*
			if !errors.Is(err, ErrTicketMalformed) {
				t.Errorf("%s: err = %v, want ErrTicketMalformed", v.Name, err)
			}
		}
	}
}

// TestValidateTicketOrder pins the spec §5 validation order
// (shape->email->expiry->time->nonce/mac->HMAC) with first-failure-wins.
func TestValidateTicketOrder(t *testing.T) {
	secret := testSecret(t)
	const live = 2000000000
	const nowUnix = 1500000000
	mint := func(email string, expiry int64, nonce string) string {
		return MintTicket(email, expiry, nonce, secret)
	}
	goodNonce := "9f8e7d6c5b4a39281706f5e4d3c2b1a0"
	cases := []struct {
		name   string
		ticket string
		want   error
	}{
		{"empty", "", ErrTicketMalformed},
		{"two segments", "a.b", ErrTicketMalformed},
		{"five segments", "a.b.c.d.e", ErrTicketMalformed},
		{"empty segment", "a..c.d", ErrTicketMalformed},
		{"bad base64", "!!!.2000000000." + goodNonce + ".00", ErrTicketMalformed},
		{"non-canonical base64 pads", "b3duZXJAZXhhbXBsZS5jb20=.2000000000." + goodNonce + ".00", ErrTicketMalformed},
		{"bad email shape", MintTicket("not-an-email", live, goodNonce, secret), ErrTicketMalformed},
		{"bad expiry alpha", strings.Replace(mint("owner@example.com", live, goodNonce), ".2000000000.", ".soon.", 1), ErrTicketMalformed},
		{"expiry plus sign", strings.Replace(mint("owner@example.com", live, goodNonce), ".2000000000.", ".+2000000000.", 1), ErrTicketMalformed},
		{"expiry minus sign", strings.Replace(mint("owner@example.com", live, goodNonce), ".2000000000.", ".-5.", 1), ErrTicketMalformed},
		{"expiry 11 digits", strings.Replace(mint("owner@example.com", live, goodNonce), ".2000000000.", ".20000000000.", 1), ErrTicketMalformed},
		{"expired beats bad nonce", MintTicket("owner@example.com", 1000000000, "ZZZ-not-hex", secret), ErrTicketExpired},
		{"uppercase nonce", MintTicket("owner@example.com", live, "9F8E7D6C5B4A39281706F5E4D3C2B1A0", secret), ErrTicketMalformed},
		{"short nonce", MintTicket("owner@example.com", live, "abcd", secret), ErrTicketMalformed},
		{"uppercase mac passes shape but fails hmac", "b3duZXJAZXhhbXBsZS5jb20.2000000000." + goodNonce + ".465DCAC77DF0660FB921AFAB539AB3B6AB766B5149317C06EC148A68B2081A5F", ErrTicketMalformed},
		{"wrong secret", func() string {
			other := make([]byte, 32)
			for i := range other {
				other[i] = byte(i + 1)
			}
			return MintTicket("owner@example.com", live, goodNonce, other)
		}(), ErrTicketTampered},
	}
	for _, c := range cases {
		if _, err := ValidateTicket(c.ticket, secret, nowUnix); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Malformed and tampered share one body (no oracle).
	if ErrTicketMalformed.Error() != "invalid ticket: malformed or tampered." ||
		ErrTicketTampered.Error() != "invalid ticket: malformed or tampered." ||
		ErrTicketExpired.Error() != "invalid ticket: expired." {
		t.Error("error bodies drifted from spec §6")
	}
}

// TestParseSecretHex pins startup fail-fast inputs.
func TestParseSecretHex(t *testing.T) {
	if b, err := ParseSecretHex(testSecretHex); err != nil || len(b) != 32 {
		t.Fatalf("valid fixture secret: b=%d err=%v", len(b), err)
	}
	for _, raw := range []string{"", "abc", strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("z", 64), "  " + testSecretHex + "  \n"} {
		b, err := ParseSecretHex(raw)
		if strings.TrimSpace(raw) == testSecretHex {
			if err != nil || len(b) != 32 {
				t.Errorf("padded fixture secret: b=%d err=%v, want 32 nil", len(b), err)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseSecretHex(%q): got nil error, want fail-fast", raw)
		}
	}
}

// TestParseAuthMode pins standard-default + sso opt-in.
func TestParseAuthMode(t *testing.T) {
	for in, want := range map[string]string{
		"": ModeStandard, "standard": ModeStandard, "STANDARD": ModeStandard,
		" sso ": ModeSSO, "SSO": ModeSSO, "sso": ModeSSO,
		"sso2": ModeStandard, "off": ModeStandard, "cloud": ModeStandard,
	} {
		if got := ParseAuthMode(in); got != want {
			t.Errorf("ParseAuthMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNonceCacheClaim pins single-use + TTL expiry + sweep.
func TestNonceCacheClaim(t *testing.T) {
	c := NewNonceCache(50 * time.Millisecond)
	if c.Claim("n1") {
		t.Fatal("first claim must succeed")
	}
	if !c.Claim("n1") {
		t.Fatal("second claim must report replay")
	}
	time.Sleep(120 * time.Millisecond)
	if c.Claim("n1") {
		t.Fatal("claim after TTL must succeed again (entry expired)")
	}
}

// ssoTestApp boots a TestApp with a clean _superusers table.
func ssoTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	recs, err := app.FindAllRecords(superusersCollection)
	if err != nil {
		t.Fatalf("list superusers: %v", err)
	}
	unsafe := app.UnsafeWithoutHooks()
	for _, r := range recs {
		if err := unsafe.Delete(r); err != nil {
			t.Fatalf("delete superuser %s: %v", r.Id, err)
		}
	}
	return app
}

func createTestSuperuser(t *testing.T, app *tests.TestApp, email string) {
	t.Helper()
	testhelp.MustSuperuser(t, app, email)
}

// testServer builds a handler server with a fresh cache.
func testServer(mode string, secret []byte) *server {
	return &server{mode: mode, secret: secret, cache: NewNonceCache(NonceTTL)}
}

// callRedeem invokes postRedeem directly, reporting status + raw body.
func callRedeem(app core.App, srv *server, body string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sso/redeem", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := srv.postRedeem(re); err != nil {
		return testhelp.CodeMessage(err, rec)
	}
	return rec.Code, rec.Body.String()
}

// callMode invokes getMode directly.
func callMode(app core.App, srv *server) (int, string) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/mode", nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := srv.getMode(re); err != nil {
		return -1, err.Error()
	}
	return rec.Code, rec.Body.String()
}

// TestRedeemHandlerMatrix pins status codes + exact spec §6 bodies.
func TestRedeemHandlerMatrix(t *testing.T) {
	app := ssoTestApp(t)
	createTestSuperuser(t, app, "owner@example.com")
	secret := testSecret(t)
	srv := testServer(ModeSSO, secret)

	// valid vector (expiry 2000000000 is live) -> 200 {token}.
	vecs := loadVectors(t)
	var validTicket string
	for _, v := range vecs {
		if v.Name == "valid" {
			validTicket = v.Ticket
		}
	}
	code, body := callRedeem(app, srv, `{"ticket":`+quoteJSON(validTicket)+`}`)
	if code != http.StatusOK {
		t.Fatalf("valid redeem: status=%d body=%s, want 200", code, body)
	}
	var okBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &okBody); err != nil || okBody.Token == "" {
		t.Fatalf("valid redeem body=%q, want {token}", body)
	}

	// Replay of the same ticket -> 409 (nonce already claimed).
	code, body = callRedeem(app, srv, `{"ticket":`+quoteJSON(validTicket)+`}`)
	if code != http.StatusConflict || !strings.Contains(body, "ticket already redeemed.") {
		t.Fatalf("replay: status=%d body=%s, want 409 ticket already redeemed.", code, body)
	}
	if strings.Contains(body, validTicket) {
		t.Fatalf("replay body echoes the ticket: %s", redactLen(body))
	}

	for _, v := range vecs {
		switch v.Name {
		case "valid":
			continue // covered above (now replayed — skip)
		case "expired":
			code, body = callRedeem(app, srv, `{"ticket":`+quoteJSON(v.Ticket)+`}`)
			if code != http.StatusBadRequest || !strings.Contains(body, "invalid ticket: expired.") {
				t.Errorf("expired: status=%d body=%s, want 400 expired.", code, redactLen(body))
			}
		case "tampered-mac":
			code, body = callRedeem(app, srv, `{"ticket":`+quoteJSON(v.Ticket)+`}`)
			if code != http.StatusBadRequest || !strings.Contains(body, "invalid ticket: malformed or tampered.") {
				t.Errorf("tampered: status=%d body=%s, want 400 generic.", code, redactLen(body))
			}
		default:
			code, body = callRedeem(app, srv, `{"ticket":`+quoteJSON(v.Ticket)+`}`)
			if code != http.StatusBadRequest || !strings.Contains(body, "invalid ticket: malformed or tampered.") {
				t.Errorf("%s: status=%d body=%s, want 400 generic.", v.Name, code, redactLen(body))
			}
		}
	}

	// Unknown user: fresh valid-MAC ticket for a ghost email -> 404.
	nonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	ghost := MintTicket("ghost@example.com", time.Now().Unix()+60, nonce, secret)
	code, body = callRedeem(app, srv, `{"ticket":`+quoteJSON(ghost)+`}`)
	if code != http.StatusNotFound || !strings.Contains(body, "unknown user.") {
		t.Errorf("unknown user: status=%d body=%s, want 404 unknown user.", code, redactLen(body))
	}

	// Empty JSON body -> 400 (decodeBody path, never a token).
	code, _ = callRedeem(app, srv, ``)
	if code != http.StatusBadRequest {
		t.Errorf("empty body: status=%d, want 400", code)
	}
}

// TestRedeemUnknownUserAfterMAC pins 404-after-MAC ordering (no oracle):
// a tampered ticket for a ghost email reads 400, not 404.
func TestRedeemUnknownUserAfterMAC(t *testing.T) {
	app := ssoTestApp(t)
	srv := testServer(ModeSSO, testSecret(t))
	nonce, _ := GenerateNonce()
	bad := MintTicket("ghost@example.com", time.Now().Unix()+60, nonce, testSecret(t)) + "x"
	code, body := callRedeem(app, srv, `{"ticket":`+quoteJSON(bad)+`}`)
	if code != http.StatusBadRequest {
		t.Fatalf("tampered ghost ticket: status=%d body=%s, want 400 (MAC before user)", code, redactLen(body))
	}
}

// TestModeEndpoint pins the UI mode signal shape.
func TestModeEndpoint(t *testing.T) {
	app := ssoTestApp(t)
	code, body := callMode(app, testServer(ModeSSO, testSecret(t)))
	if code != http.StatusOK || !strings.Contains(body, `"mode":"sso"`) {
		t.Errorf("sso mode: status=%d body=%s", code, body)
	}
	code, body = callMode(app, testServer(ModeStandard, nil))
	if code != http.StatusOK || !strings.Contains(body, `"mode":"standard"`) {
		t.Errorf("standard mode: status=%d body=%s", code, body)
	}
	srv := testServer(ModeSSO, testSecret(t))
	srv.cloudURL = "https://cloud.example.com"
	if _, body := callMode(app, srv); !strings.Contains(body, `"cloudUrl":"https://cloud.example.com"`) {
		t.Errorf("cloudUrl missing from mode body: %s", body)
	}
}

// TestStandardModeRedeemDisabled pins the standard-default regression:
// redeem exists but never issues (404, no token), so existing auth paths
// are untouched when the flag is unset.
func TestStandardModeRedeemDisabled(t *testing.T) {
	app := ssoTestApp(t)
	createTestSuperuser(t, app, "owner@example.com")
	secret := testSecret(t)
	nonce, _ := GenerateNonce()
	ticket := MintTicket("owner@example.com", time.Now().Unix()+60, nonce, secret)
	srv := testServer(ModeStandard, nil)
	code, body := callRedeem(app, srv, `{"ticket":`+quoteJSON(ticket)+`}`)
	if code != http.StatusNotFound || !strings.Contains(body, "sso not enabled.") {
		t.Fatalf("standard redeem: status=%d body=%s, want 404 sso not enabled.", code, body)
	}
	if strings.Contains(body, "token") {
		t.Fatalf("standard redeem must never issue a token: %s", redactLen(body))
	}
}

func quoteJSON(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// redactLen avoids printing ticket bodies on failure (tickets are
// short-lived but still secrets in logs).
func redactLen(s string) string {
	return "(body " + strconv.Itoa(len(s)) + " bytes, redacted)"
}
