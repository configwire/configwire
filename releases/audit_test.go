package releases

// Audit trail wiring: publish, rollback and promote each emit exactly one
// audit_log row whose target/status/detail match the existing stderr
// lines.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"github.com/configwire/configwire/internal/testhelp"
)

func releaseAuditRows(t *testing.T, app core.App, action string) []*core.Record {
	t.Helper()
	rows, err := app.FindRecordsByFilter("audit_log", "action = {:a}", "", 0, 0, dbx.Params{"a": action})
	if err != nil {
		t.Fatalf("find audit %s: %v", action, err)
	}
	return rows
}

func releaseAuditDetail(t *testing.T, r *core.Record) map[string]any {
	t.Helper()
	raw, err := json.Marshal(r.GetRaw("detail"))
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal detail %s: %v", raw, err)
	}
	return out
}

func callRollbackHandler(t *testing.T, app *tests.TestApp, slug string, version int) (int, map[string]any, string) {
	t.Helper()
	url := "/api/v1/admin/env/" + slug + "/releases/" + itoa(version) + "/rollback"
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"note":"rollback note"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("env", slug)
	req.SetPathValue("version", itoa(version))
	rec := httptest.NewRecorder()
	re := &core.RequestEvent{App: app}
	re.Request = req
	re.Response = rec
	if err := postRollbackEnv(re); err != nil {
		code, msg := testhelp.CodeMessage(err, rec)
		return code, nil, msg
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode rollback body: %v", err)
	}
	return rec.Code, decoded, ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestAuditReleaseActions(t *testing.T) {
	app := promoteHandlerTestApp(t)
	testhelp.EnsureAuditCollection(t, app)

	project := seedPromoteProject(t, app, "audit-project")
	dev := seedPromoteEnv(t, app, project, "dev")
	prod := seedPromoteEnv(t, app, project, "prod")
	seedPromoteFlag(t, app, project, "checkout")

	if code, _, msg := callPublish(t, app, "dev", "", `{"note":"first publish","baseVersion":0}`); code != http.StatusOK {
		t.Fatalf("publish = %d %q, want 200", code, msg)
	}
	pub := releaseAuditRows(t, app, "release.publish")
	if len(pub) != 1 {
		t.Fatalf("release.publish rows = %d, want 1", len(pub))
	}
	if got := pub[0].GetString("targetId"); got != dev {
		t.Errorf("publish targetId = %q, want %q", got, dev)
	}
	if got := pub[0].GetString("targetLabel"); got != "dev" {
		t.Errorf("publish targetLabel = %q, want dev", got)
	}
	if got := pub[0].GetInt("status"); got != http.StatusOK {
		t.Errorf("publish status = %d, want 200", got)
	}
	d := releaseAuditDetail(t, pub[0])
	if d["env"] != "dev" || d["version"] != float64(1) || d["note"] != "first publish" {
		t.Errorf("publish detail = %v", d)
	}
	if d["etag"] == "" || d["etag"] == nil {
		t.Errorf("publish detail lacks etag: %v", d)
	}

	if code, _, msg := callRollbackHandler(t, app, "dev", 1); code != http.StatusOK {
		t.Fatalf("rollback = %d %q, want 200", code, msg)
	}
	rb := releaseAuditRows(t, app, "release.rollback")
	if len(rb) != 1 {
		t.Fatalf("release.rollback rows = %d, want 1", len(rb))
	}
	if got := rb[0].GetString("targetId"); got != dev {
		t.Errorf("rollback targetId = %q, want %q", got, dev)
	}
	d = releaseAuditDetail(t, rb[0])
	if d["fromVersion"] != float64(1) || d["version"] != float64(2) || d["note"] != "rollback note" {
		t.Errorf("rollback detail = %v", d)
	}

	if code, _, msg := callPromote(t, app, "prod", "", `{"srcEnv":"dev","srcVersion":1,"destBaseVersion":0,"note":"promote note"}`); code != http.StatusOK {
		t.Fatalf("promote = %d %q, want 200", code, msg)
	}
	pr := releaseAuditRows(t, app, "release.promote")
	if len(pr) != 1 {
		t.Fatalf("release.promote rows = %d, want 1", len(pr))
	}
	if got := pr[0].GetString("targetId"); got != prod {
		t.Errorf("promote targetId = %q, want %q", got, prod)
	}
	if got := pr[0].GetString("targetLabel"); got != "prod" {
		t.Errorf("promote targetLabel = %q, want prod", got)
	}
	d = releaseAuditDetail(t, pr[0])
	if d["srcEnv"] != dev || d["destEnv"] != prod || d["srcVersion"] != float64(1) ||
		d["version"] != float64(1) || d["note"] != "promote note" {
		t.Errorf("promote detail = %v", d)
	}

	// Detail never carries the snapshot.
	for _, r := range append(append(pub, rb...), pr...) {
		if raw, _ := json.Marshal(r.GetRaw("detail")); strings.Contains(string(raw), "snapshot") {
			t.Errorf("audit detail leaked the snapshot: %s", raw)
		}
	}
}
