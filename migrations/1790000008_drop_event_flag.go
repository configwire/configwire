package migrations

// Drop analytics flag attribution from events/event_daily.
//
// Stats are env-wide (no per-flag filtering; a stale `flag` stats param is
// ignored) and ingest/purge no longer read or write the flag attribution,
// so the storage goes away here:
//   - events.flag and event_daily.flag relation fields are removed. Cascade
//     needs no separate cleanup: cascade is enforced via the relation
//     fields' CascadeDelete flag (see 1790000006_cascade_hierarchy.go),
//     so deleting the fields deletes the cascade entries with them.
//     rules.flag / experiments.flag targeting is untouched.
//   - idx_events_env_flag_ts is deleted (idx_events_env_ts stays).
//   - idx_event_daily_upsert_key is redefined flagless as UNIQUE
//     (day, env, variant, version).
//
// Old rows' flag values were analytics-only attribution with no consumers;
// their loss is accepted and intended. Down re-adds the relation fields
// (nullable, cascade-delete, as originally defined) and the old indexes as
// a best-effort inverse so `migrate down` works; it cannot restore the
// dropped values.

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func foldEventDaily(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("event_daily"); err != nil {
		return nil // collection absent; keep up idempotent
	}
	recs, err := app.FindAllRecords("event_daily")
	if err != nil {
		if _, cerr := app.FindCollectionByNameOrId("event_daily"); cerr != nil {
			return nil
		}
		return err
	}
	type foldKey struct {
		day     string
		env     string
		variant string
		version int
	}
	groups := map[foldKey][]*core.Record{}
	for _, r := range recs {
		k := foldKey{
			day:     r.GetDateTime("day").Time().UTC().Format(time.DateOnly),
			env:     r.GetString("env"),
			variant: r.GetString("variant"),
			version: r.GetInt("version"),
		}
		groups[k] = append(groups[k], r)
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		totalF, totalE := 0, 0
		for _, r := range g {
			totalF += r.GetInt("fetches")
			totalE += r.GetInt("exposures")
		}
		survivor := g[0]
		for _, r := range g[1:] {
			st, rt := survivor.GetDateTime("created").Time(), r.GetDateTime("created").Time()
			if rt.Before(st) || (rt.Equal(st) && r.Id < survivor.Id) {
				survivor = r
			}
		}
		survivor.Set("fetches", totalF)
		survivor.Set("exposures", totalE)
		if err := app.Save(survivor); err != nil {
			return err
		}
		for _, r := range g {
			if r.Id == survivor.Id {
				continue
			}
			if err := app.Delete(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func dropEventFlagUp(app core.App, collectionName string) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return nil // collection absent; keep up idempotent
	}
	if collectionName == "events" {
		collection.RemoveIndex("idx_events_env_flag_ts")
	}
	if collectionName == "event_daily" {
		if err := foldEventDaily(app); err != nil {
			return err
		}
		collection.RemoveIndex("idx_event_daily_upsert_key")
		collection.AddIndex("idx_event_daily_upsert_key", true, "day, env, variant, version", "")
	}
	if collection.Fields.GetByName("flag") != nil {
		collection.Fields.RemoveByName("flag")
	}
	return app.Save(collection)
}

func addEventFlagDown(app core.App, collectionName string) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return nil
	}
	if collection.Fields.GetByName("flag") == nil {
		collection.Fields.Add(&core.RelationField{
			Name:          "flag",
			CollectionId:  colFlags,
			MaxSelect:     1,
			Required:      false,
			CascadeDelete: true,
		})
	}
	if collectionName == "events" {
		collection.AddIndex("idx_events_env_flag_ts", false, "env, flag, ts", "")
	}
	if collectionName == "event_daily" {
		collection.AddIndex("idx_event_daily_upsert_key", true, "day, env, flag, variant, version", "")
	}
	return app.Save(collection)
}

func init() {
	m.Register(func(app core.App) error {
		if err := dropEventFlagUp(app, "events"); err != nil {
			return err
		}
		return dropEventFlagUp(app, "event_daily")
	}, func(app core.App) error {
		if err := addEventFlagDown(app, "events"); err != nil {
			return err
		}
		return addEventFlagDown(app, "event_daily")
	})
}
