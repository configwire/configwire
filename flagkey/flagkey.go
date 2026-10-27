// Package flagkey is the single home of the flag-key length/shape
// contract. Publish hooks (main.go), the publish dry-assemble
// (releases.ValidateSnapshot), ingest event bodies, and the stats
// ?flag= filter must all enforce the same bound so a published flag
// is always ingestible and stats-queryable.
//
// NOTE: the initial migration (1787875200_init_configwire.go) declares
// flags.key as Text max 128, matching this bound.
package flagkey

import (
	"fmt"
	"regexp"
)

// MaxLen bounds flag keys in every surface (publish, ingest, stats);
// longer keys are rejected with 400. It matches the migration max (128)
// and the original publish bound; ingest + stats were widened to meet it.
const MaxLen = 128

// Pattern is the allowed flag-key shape, shared by the publish write
// path and the publish dry-assemble so they reject exactly the same
// keys.
var Pattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// Check validates full key shape: 1-MaxLen bytes matching Pattern.
func Check(key string) error {
	if len(key) == 0 || len(key) > MaxLen || !Pattern.MatchString(key) {
		return fmt.Errorf("invalid flag key: must match ^[A-Za-z_][A-Za-z0-9_.-]*$ and be 1-%d chars", MaxLen)
	}
	return nil
}
