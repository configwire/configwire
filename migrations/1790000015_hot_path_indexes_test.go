package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"
)

// TestHotPathIndexesPresent pins that the round-2 lookup indexes exist
// after migrations: the SDK auth prefilter and the publish/release
// scans must not fall back to full-table scans.
func TestHotPathIndexesPresent(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	for collection, index := range map[string]string{
		"sdk_keys":    "idx_sdk_keys_prefix",
		"releases":    "idx_releases_env_version",
		"rules":       "idx_rules_flag",
		"experiments": "idx_experiments_flag",
	} {
		col, err := app.FindCollectionByNameOrId(collection)
		if err != nil {
			t.Fatalf("find %s: %v", collection, err)
		}
		if got := col.GetIndex(index); got == "" {
			t.Errorf("%s: index %q missing after migration", collection, index)
		}
	}
}
