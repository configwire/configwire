package identity

import (
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// pbSaltTestApp builds a TestApp carrying the identity_salts collection
// shaped like migrations/1790000018 (envID/day/salt TEXT + UNIQUE
// (envID, day) backstop) so the PBStore tests run against the real
// filter/index behavior.
func pbSaltTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("identity_salts")
	col.Fields.Add(
		&core.TextField{Name: "envID", Required: true},
		&core.TextField{Name: "day", Required: true},
		&core.TextField{Name: "salt", Required: true},
	)
	col.AddIndex("idx_identity_salts_env_day", true, "envID, day", "")
	if err := app.Save(col); err != nil {
		t.Fatalf("save identity_salts collection: %v", err)
	}
	return app
}

// pbSaltBareApp builds a TestApp WITHOUT the identity_salts collection
// (migration not yet applied) for the missing-collection grace path.
func pbSaltBareApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	if _, err := app.FindCollectionByNameOrId("identity_salts"); err == nil {
		t.Skip("test data already carries identity_salts; grace path not exercisable here")
	}
	return app
}

func TestPBStoreLoadMiss(t *testing.T) {
	st := NewPBStore(pbSaltTestApp(t))
	salt, ok, err := st.Load("env-a", "2026-05-10")
	if err != nil {
		t.Fatalf("Load miss: %v", err)
	}
	if ok || salt != nil {
		t.Fatalf("Load miss = (%x, %v), want (nil, false)", salt, ok)
	}
}

func TestPBStoreSaveRoundtrip(t *testing.T) {
	app := pbSaltTestApp(t)
	st := NewPBStore(app)
	want, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save("env-a", "2026-05-10", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := st.Load("env-a", "2026-05-10")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load after Save: ok=false, want true")
	}
	if string(got) != string(want) {
		t.Fatal("Load after Save returned different bytes")
	}
}

func mustDay(t *testing.T, day string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestPBStoreSaveDuplicateMapsErrSaltExists(t *testing.T) {
	st := NewPBStore(pbSaltTestApp(t))
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save("env-a", "2026-05-10", salt); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	other, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save("env-a", "2026-05-10", other); !errors.Is(err, ErrSaltExists) {
		t.Fatalf("second Save = %v, want ErrSaltExists", err)
	}
	// Same day, different env is a distinct pair.
	if err := st.Save("env-b", "2026-05-10", other); err != nil {
		t.Fatalf("cross-env Save: %v", err)
	}
}

func TestPBStoreDeleteOlderThanCutoff(t *testing.T) {
	st := NewPBStore(pbSaltTestApp(t))
	seed := func(env, day string) {
		t.Helper()
		salt, err := GenerateSalt()
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Save(env, day, salt); err != nil {
			t.Fatalf("seed %s/%s: %v", env, day, err)
		}
	}
	for _, d := range []string{"2026-05-06", "2026-05-07", "2026-05-08", "2026-05-09", "2026-05-10"} {
		seed("env-a", d)
	}
	seed("env-b", "2026-05-06") // other env, older than cutoff: survives

	n, err := st.DeleteOlderThan("env-a", "2026-05-08")
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteOlderThan deleted %d rows, want 2", n)
	}
	for _, d := range []string{"2026-05-08", "2026-05-09", "2026-05-10"} {
		if _, ok, _ := st.Load("env-a", d); !ok {
			t.Fatalf("day %s pruned but must be kept", d)
		}
	}
	for _, d := range []string{"2026-05-06", "2026-05-07"} {
		if _, ok, _ := st.Load("env-a", d); ok {
			t.Fatalf("day %s kept but must be deleted", d)
		}
	}
	if _, ok, _ := st.Load("env-b", "2026-05-06"); !ok {
		t.Fatal("env-b row deleted by env-a prune, want env scoping")
	}
}

func TestPBStoreMissingCollectionGraceful(t *testing.T) {
	st := NewPBStore(pbSaltBareApp(t))
	if _, ok, err := st.Load("env-a", "2026-05-10"); err != nil || ok {
		t.Fatalf("missing-collection Load = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	if n, err := st.DeleteOlderThan("env-a", "2026-05-08"); err != nil || n != 0 {
		t.Fatalf("missing-collection DeleteOlderThan = (%d, %v), want (0, nil)", n, err)
	}
}

func TestPBStoreManagerPruneDelegates(t *testing.T) {
	app := pbSaltTestApp(t)
	m := NewManager(NewPBStore(app))
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	for _, d := range []string{"2026-05-06", "2026-05-07", "2026-05-08", "2026-05-09", "2026-05-10"} {
		if _, err := m.SaltForDay("env-a", mustDay(t, d)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := m.Prune("env-a", now)
	if err != nil {
		t.Fatalf("Manager.Prune over PBStore: %v", err)
	}
	if n != 2 {
		t.Fatalf("Manager.Prune deleted %d rows, want 2 (days < DayString(now-48h))", n)
	}
	for _, d := range []string{"2026-05-08", "2026-05-09", "2026-05-10"} {
		if _, ok, _ := NewPBStore(app).Load("env-a", d); !ok {
			t.Fatalf("day %s pruned but yesterday-or-newer must survive", d)
		}
	}
	if rerun, err := m.Prune("env-a", now); err != nil || rerun != 0 {
		t.Fatalf("rerun Manager.Prune = %d, err=%v; want 0 (converges)", rerun, err)
	}
}

func TestPBStoreManagerRoundtrip(t *testing.T) {
	app := pbSaltTestApp(t)
	m := NewManager(NewPBStore(app))
	day := "2026-05-10"
	a, err := m.SaltForDay("env-a", mustDay(t, day))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.SaltForDay("env-a", mustDay(t, day))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("same UTC day returned different salts via PBStore")
	}
	// Stored TEXT is hex of the 32B salt.
	recs, err := app.FindRecordsByFilter("identity_salts", "envID = {:env} && day = {:day}", "", 1, 0, dbx.Params{"env": "env-a", "day": day})
	if err != nil {
		t.Fatalf("read back row: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("identity_salts rows = %d, want 1", len(recs))
	}
	raw, err := hex.DecodeString(recs[0].GetString("salt"))
	if err != nil {
		t.Fatalf("stored salt is not hex TEXT: %v", err)
	}
	if len(raw) != SaltLen || string(raw) != string(a) {
		t.Fatal("stored hex does not decode to the served salt")
	}
}
