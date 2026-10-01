package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestSDKKeyVerifierSchema(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	vf := col.Fields.GetByName("verifier")
	if vf == nil {
		t.Fatal("sdk_keys.verifier missing after migration")
	}
	if tf, ok := vf.(*core.TextField); !ok {
		t.Fatalf("verifier is %T, want *core.TextField", vf)
	} else if tf.Required {
		t.Error("verifier must be optional")
	}
	kf := col.Fields.GetByName("keyVer")
	if kf == nil {
		t.Fatal("sdk_keys.keyVer missing after migration")
	}
	if _, ok := kf.(*core.NumberField); !ok {
		t.Fatalf("keyVer is %T, want *core.NumberField", kf)
	}
	hf := col.Fields.GetByName("hash")
	if hf == nil {
		t.Fatal("sdk_keys.hash missing after migration")
	}
	if tf, ok := hf.(*core.TextField); !ok {
		t.Fatalf("hash is %T, want *core.TextField", hf)
	} else if tf.Required {
		t.Error("hash must be optional so v2 rows can store empty")
	}
}

func TestSDKKeyVerifierRows(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col, err := app.FindCollectionByNameOrId("sdk_keys")
	if err != nil {
		t.Fatalf("find sdk_keys: %v", err)
	}
	pcol, err := app.FindCollectionByNameOrId("projects")
	if err != nil {
		t.Fatalf("find projects: %v", err)
	}
	proj := core.NewRecord(pcol)
	proj.Set("name", "verifier-proj")
	if err := app.Save(proj); err != nil {
		t.Fatalf("save project: %v", err)
	}
	ecol, err := app.FindCollectionByNameOrId("environments")
	if err != nil {
		t.Fatalf("find environments: %v", err)
	}
	env := core.NewRecord(ecol)
	env.Set("project", proj.Id)
	env.Set("slug", "v2test")
	if err := app.Save(env); err != nil {
		t.Fatalf("save env: %v", err)
	}
	legacy := core.NewRecord(col)
	legacy.Set("prefix", "cw-legac")
	legacy.Set("hash", "abc123")
	legacy.Set("env", env.Id)
	if err := app.Save(legacy); err != nil {
		t.Fatalf("legacy row must still save: %v", err)
	}
	back, err := app.FindRecordById("sdk_keys", legacy.Id)
	if err != nil {
		t.Fatalf("legacy row must still read: %v", err)
	}
	if back.GetString("hash") != "abc123" {
		t.Fatalf("legacy hash = %q, want abc123", back.GetString("hash"))
	}
	v2 := core.NewRecord(col)
	v2.Set("prefix", "cw-v2key-")
	v2.Set("hash", "")
	v2.Set("env", env.Id)
	v2.Set("keyVer", 2)
	v2.Set("verifier", "$2a$10$testverifierplaceholder0000000000000000000000")
	if err := app.Save(v2); err != nil {
		t.Fatalf("v2 row with empty hash must save: %v", err)
	}
}
