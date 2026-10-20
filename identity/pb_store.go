package identity

import (
	"encoding/hex"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const pbSaltCollection = "identity_salts"

const pbSaltPageSize = 2000

var _ Store = (*PBStore)(nil)

type PBStore struct {
	app core.App
}

func NewPBStore(app core.App) *PBStore {
	return &PBStore{app: app}
}

func (s *PBStore) Load(envID, day string) ([]byte, bool, error) {
	if _, err := s.app.FindCollectionByNameOrId(pbSaltCollection); err != nil {
		return nil, false, nil
	}
	recs, err := s.app.FindRecordsByFilter(
		pbSaltCollection,
		"envID = {:env} && day = {:day}",
		"", 1, 0,
		dbx.Params{"env": envID, "day": day},
	)
	if err != nil {
		return nil, false, err
	}
	if len(recs) == 0 {
		return nil, false, nil
	}
	raw, err := hex.DecodeString(recs[0].GetString("salt"))
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

func (s *PBStore) Save(envID, day string, salt []byte) error {
	col, err := s.app.FindCollectionByNameOrId(pbSaltCollection)
	if err != nil {
		return err
	}
	existing, err := s.app.FindRecordsByFilter(
		pbSaltCollection,
		"envID = {:env} && day = {:day}",
		"", 1, 0,
		dbx.Params{"env": envID, "day": day},
	)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return ErrSaltExists
	}
	rec := core.NewRecord(col)
	rec.Set("envID", envID)
	rec.Set("day", day)
	rec.Set("salt", hex.EncodeToString(salt))
	if err := s.app.Save(rec); err != nil {
		if isUniqueViolation(err) {
			return ErrSaltExists
		}
		return err
	}
	return nil
}

func (s *PBStore) DeleteOlderThan(envID, cutoffDay string) (int, error) {
	if _, err := s.app.FindCollectionByNameOrId(pbSaltCollection); err != nil {
		return 0, nil
	}
	base := "envID = {:env} && day != '' && day < {:cutoff}"
	n := 0
	var cursorDay, cursorID string
	hasCursor := false
	for {
		params := dbx.Params{"env": envID, "cutoff": cutoffDay}
		filter := base
		if hasCursor {
			params["cursor"] = cursorDay
			params["cursorID"] = cursorID
			filter += " && (day > {:cursor} || (day = {:cursor} && id > {:cursorID}))"
		}
		page, err := s.app.FindRecordsByFilter(pbSaltCollection, filter, "day,id", pbSaltPageSize, 0, params)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		var doomed []*core.Record
		for _, r := range page {
			if r.GetString("envID") != envID {
				continue
			}
			if day := r.GetString("day"); day != "" && day < cutoffDay {
				doomed = append(doomed, r)
			}
		}
		last := page[len(page)-1]
		cursorDay = last.GetString("day")
		cursorID = last.Id
		hasCursor = true
		if len(doomed) > 0 {
			pageDeleted := 0
			if err := s.app.RunInTransaction(func(txApp core.App) error {
				for _, r := range doomed {
					if err := txApp.Delete(r); err != nil {
						return err
					}
					pageDeleted++
				}
				return nil
			}); err != nil {
				return 0, err
			}
			n += pageDeleted
		}
		if len(page) < pbSaltPageSize {
			break
		}
	}
	return n, nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
