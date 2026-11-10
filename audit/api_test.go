package audit

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"
)

func TestParseSinceGrammar(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		want   int
		wantOK bool
	}{
		{"", 30, true},
		{"7d", 7, true},
		{"30d", 30, true},
		{"1825d", 1825, true},
		{"all", 0, true},
		{"0", 0, true},
		{"0d", 0, false},
		{"1826d", 0, false},
		{"30", 0, false},
		{"abc", 0, false},
		{"7w", 0, false},
	} {
		got, ok := parseSince(tc.raw)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("parseSince(%q) = (%d,%v), want (%d,%v)", tc.raw, got, ok, tc.want, tc.wantOK)
		}
	}
}

type auditAPIResponse struct {
	Items []struct {
		ID          string          `json:"id"`
		Actor       string          `json:"actor"`
		Action      string          `json:"action"`
		TargetType  string          `json:"targetType"`
		TargetID    string          `json:"targetId"`
		TargetLabel string          `json:"targetLabel"`
		Status      int             `json:"status"`
		Detail      json.RawMessage `json:"detail"`
		Created     string          `json:"created"`
	} `json:"items"`
	Page    int `json:"page"`
	PerPage int `json:"perPage"`
	Total   int `json:"total"`
}

func callAudit(t *testing.T, app *core.BaseApp, query string) (*httptest.ResponseRecorder, auditAPIResponse, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?"+query, nil)
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	err := getAudit(re)
	var out auditAPIResponse
	if err == nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec, out, err
}

func TestAuditAPIResponseShape(t *testing.T) {
	app := newAuditTestApp(t)
	mustWrite(t, app, Entry{
		Actor: "admin@example.com", ActorID: "u1", Action: "key.mint",
		TargetType: "sdk_key", TargetID: "k1", TargetLabel: "cw-AAAA1111",
		Status: 201,
		Detail: map[string]any{"env": "e1", "fetchRps": 100, "ingestRps": 50},
	})

	rec, out, err := callAudit(t, app.BaseApp, "action=key.mint")
	if err != nil {
		t.Fatalf("getAudit: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if out.Page != 1 || out.PerPage != 50 || out.Total != 1 {
		t.Errorf("pager = %d/%d total %d, want 1/50 total 1", out.Page, out.PerPage, out.Total)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Items))
	}
	it := out.Items[0]
	if it.Actor != "admin@example.com" || it.Action != "key.mint" || it.Status != 201 {
		t.Errorf("item = %+v", it)
	}
	if it.TargetType != "sdk_key" || it.TargetID != "k1" || it.TargetLabel != "cw-AAAA1111" {
		t.Errorf("target = %+v", it)
	}
	if it.Created == "" {
		t.Error("created is empty")
	}
	var detail map[string]any
	if err := json.Unmarshal(it.Detail, &detail); err != nil {
		t.Fatalf("detail = %s: %v", it.Detail, err)
	}
	if detail["env"] != "e1" || detail["fetchRps"] != float64(100) {
		t.Errorf("detail = %v", detail)
	}
}

func TestAuditAPIFiltersPagerTotal(t *testing.T) {
	app := newAuditTestApp(t)
	for _, e := range []Entry{
		{Actor: "a@example.com", Action: "session.login", TargetType: "account", Status: 200},
		{Actor: "a@example.com", Action: "key.mint", TargetType: "sdk_key", TargetID: "k1", Status: 201},
		{Actor: "a@example.com", Action: "key.mint", TargetType: "sdk_key", TargetID: "k2", Status: 201},
		{Actor: "b@example.com", Action: "session.login_failed", TargetType: "account", Status: 401},
	} {
		mustWrite(t, app, e)
	}

	for _, tc := range []struct {
		name  string
		query string
		total int
		items int
	}{
		{"all", "since=all", 4, 4},
		{"action", "action=key.mint", 2, 2},
		{"actor", "actor=a@example.com", 3, 3},
		{"target type", "targetType=sdk_key", 2, 2},
		{"target id", "targetId=k1", 1, 1},
		{"status", "status=401", 1, 1},
		{"pager", "perPage=2&page=2", 4, 2},
		{"default since", "", 4, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, out, err := callAudit(t, app.BaseApp, tc.query)
			if err != nil {
				t.Fatalf("getAudit: %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			if out.Total != tc.total || len(out.Items) != tc.items {
				t.Errorf("total/items = %d/%d, want %d/%d", out.Total, len(out.Items), tc.total, tc.items)
			}
		})
	}

	_, out, err := callAudit(t, app.BaseApp, "perPage=999&page=1")
	if err != nil {
		t.Fatalf("getAudit: %v", err)
	}
	if out.PerPage != 200 {
		t.Errorf("perPage clamp = %d, want 200", out.PerPage)
	}
}

func TestAuditAPIRejectsBadQuery(t *testing.T) {
	app := newAuditTestApp(t)
	for _, query := range []string{
		"since=abc",
		"since=0d",
		"since=9999d",
		"page=0",
		"page=x",
		"perPage=0",
		"status=x",
		"status=-1",
	} {
		_, _, err := callAudit(t, app.BaseApp, query)
		var apiErr *router.ApiError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
			t.Errorf("%s error = %v, want 400 ApiError", query, err)
		}
	}
}
