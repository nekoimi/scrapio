package v22_query_repo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/dataquery"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_data_repo"
)

type ViewInput struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	ExpectedRevision int             `json:"expected_revision"`
	Query            dataquery.Query `json:"query"`
}

func View(ctx context.Context, owner int64, id string) (*table.V22DataView, error) {
	row := new(table.V22DataView)
	has, err := db.Instance().Context(quiet(ctx)).Where("id=? AND owner_id=?", id, owner).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, v22_data_repo.ErrNotFound
	}
	return row, nil
}
func Views(ctx context.Context, owner, id int64) ([]table.V22DataView, error) {
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if _, err := loadTable(s, owner, id); err != nil {
		return nil, err
	}
	rows := []table.V22DataView{}
	err := s.Where("owner_id=? AND table_id=?", owner, id).Asc("created_at", "id").Limit(30).Find(&rows)
	return rows, err
}
func SaveView(ctx context.Context, owner, id int64, input ViewInput) (*table.V22DataView, error) {
	if _, err := uuid.Parse(input.ID); err != nil {
		return nil, invalid(err)
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 80 || input.ExpectedRevision < 0 {
		return nil, invalid(errors.New("invalid view name or revision"))
	}
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, owner, "views.capacity", stringID(id)); err != nil {
		return nil, err
	}
	q, _, err := Normalize(s, owner, id, input.Query)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(q)
	row := new(table.V22DataView)
	has, err := s.Where("id=? AND owner_id=? AND table_id=?", input.ID, owner, id).Get(row)
	if err != nil {
		return nil, err
	}
	if has && row.Revision != input.ExpectedRevision {
		if row.Revision == input.ExpectedRevision+1 && row.Name == input.Name && equalJSON(row.Query, string(raw)) {
			return row, nil
		}
		return nil, ErrConflict
	}
	if !has {
		if input.ExpectedRevision != 0 {
			return nil, ErrConflict
		}
		count, err := s.Where("owner_id=? AND table_id=?", owner, id).Count(new(table.V22DataView))
		if err != nil {
			return nil, err
		}
		if count >= 30 {
			return nil, ErrCapacity
		}
		row.Id = input.ID
		row.OwnerId = owner
		row.TableId = id
		row.CreatedAt = time.Now().UTC()
	}
	row.Name = input.Name
	row.Revision++
	row.Query = string(raw)
	row.UpdatedAt = time.Now().UTC()
	if has {
		_, err = s.Where("id=? AND owner_id=?", row.Id, owner).Cols("name", "revision", "query", "updated_at").Update(row)
	} else {
		_, err = s.InsertOne(row)
	}
	if err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func DeleteView(ctx context.Context, owner int64, id string, revision int) error {
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if revision < 1 {
		return v22_data_repo.ErrInvalid
	}
	affected, err := s.Where("id=? AND owner_id=? AND revision=?", id, owner, revision).Delete(new(table.V22DataView))
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConflict
	}
	return nil
}
func stringID(id int64) string { return fmtID(id) }
func equalJSON(a, b string) bool {
	var x, y any
	da := json.NewDecoder(strings.NewReader(a))
	db := json.NewDecoder(strings.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	ar, _ := json.Marshal(x)
	br, _ := json.Marshal(y)
	return string(ar) == string(br)
}
