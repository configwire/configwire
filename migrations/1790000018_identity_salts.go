package migrations

// Identity daily salts (ADDITIVE only).
//
// Creates the identity_salts collection holding one row per
// (envID, UTC day) for the configwire/identity salt manager (Q1
// Aptabase-style daily rebucket): {envID Text required, day Text
// required (YYYY-MM-DD UTC), salt Text required (hex of the 32B
// crypto/rand salt — PocketBase collections have no BLOB column type,
// so the raw bytes live hex-encoded in TEXT), plus the automatic
// created/updated autodate fields every base collection carries
// (created is the createdAt)}.
//
// Uniqueness of (envID, day) is enforced by a UNIQUE index
// (idx_identity_salts_env_day): concurrent first-use creators race on
// insert and the loser re-loads the winner's salt (see
// identity.Manager.SaltForDay handling ErrSaltExists).
//
// Rules: nil-deny like every other ConfigWire collection (nil = deny
// non-superusers; "" would be public). Only server-side superuser
// context ever reads/writes these rows; SDK clients never see them.
//
// Down drops identity_salts ONLY.
import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const colIdentitySalts = "cw_identity_salts"

func init() {
	m.Register(func(app core.App) error {
		if _, err := app.FindCollectionByNameOrId("identity_salts"); err == nil {
			return nil
		}
		salts := core.NewBaseCollection("identity_salts", colIdentitySalts)
		salts.ListRule = nil
		salts.ViewRule = nil
		salts.CreateRule = nil
		salts.UpdateRule = nil
		salts.DeleteRule = nil
		salts.Fields.Add(
			&core.TextField{Name: "envID", Required: true, Presentable: true},
			&core.TextField{Name: "day", Required: true, Presentable: true},
			&core.TextField{Name: "salt", Required: true},
		)
		salts.AddIndex("idx_identity_salts_env_day", true, "envID, day", "")
		return app.Save(salts)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("identity_salts")
		if err != nil {
			return nil
		}
		return app.Delete(collection)
	})
}
