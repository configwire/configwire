package logstore

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
)

// settingsTestApp builds a TestApp with a log_settings collection shaped
// like the migrated one (key/enabled/maxDays/successLogs).
func settingsTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() {
		resetForTest(Defaults())
		app.Cleanup()
	})

	col := core.NewBaseCollection("log_settings")
	for _, f := range []string{
		`{"type":"text","name":"key","required":true}`,
		`{"type":"bool","name":"enabled"}`,
		`{"type":"number","name":"maxDays","onlyInt":true}`,
		`{"type":"bool","name":"successLogs"}`,
	} {
		if err := col.Fields.AddMarshaledJSON([]byte(f)); err != nil {
			t.Fatalf("add field %s: %v", f, err)
		}
	}
	if err := app.Save(col); err != nil {
		t.Fatalf("save log_settings collection: %v", err)
	}
	return app
}

func TestDefaultsSuccessLogsOn(t *testing.T) {
	if !Defaults().SuccessLogs {
		t.Error("Defaults().SuccessLogs = false, want true")
	}
}

func TestUpdateSettingsRoundTripsSuccessLogs(t *testing.T) {
	app := settingsTestApp(t)

	if err := UpdateSettings(app, Settings{Enabled: true, MaxDays: 30, SuccessLogs: false}); err != nil {
		t.Fatalf("UpdateSettings off: %v", err)
	}
	if GetConfig().SuccessLogs {
		t.Error("GetConfig().SuccessLogs = true after storing false, want false")
	}
	rec, err := app.FindFirstRecordByData("log_settings", "key", "global")
	if err != nil {
		t.Fatalf("find global row: %v", err)
	}
	if rec.GetBool("successLogs") {
		t.Error("row successLogs = true after storing false, want false")
	}

	if err := UpdateSettings(app, Settings{Enabled: true, MaxDays: 30, SuccessLogs: true}); err != nil {
		t.Fatalf("UpdateSettings on: %v", err)
	}
	if !GetConfig().SuccessLogs {
		t.Error("GetConfig().SuccessLogs = false after storing true, want true")
	}
}

// TestResolvePUTKeepsStoredSuccessLogs pins that absent/null keeps the
// stored value while explicit values replace it (same contract as enabled).
func TestResolvePUTKeepsStoredSuccessLogs(t *testing.T) {
	on, off := true, false
	if got := resolvePUTBool(true, nil); got != true {
		t.Errorf("resolvePUTBool(true, nil) = %v, want true", got)
	}
	if got := resolvePUTBool(false, nil); got != false {
		t.Errorf("resolvePUTBool(false, nil) = %v, want false", got)
	}
	if got := resolvePUTBool(true, &off); got != false {
		t.Errorf("resolvePUTBool(true, &off) = %v, want false", got)
	}
	if got := resolvePUTBool(false, &on); got != true {
		t.Errorf("resolvePUTBool(false, &on) = %v, want true", got)
	}
}
