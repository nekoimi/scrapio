package v22_regression_repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repair"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
)

type RestoreInput struct {
	ExpectedRevision int    `json:"expected_revision"`
	VersionID        string `json:"version_id"`
	Confirmed        bool   `json:"confirmed"`
}

func Restore(ctx context.Context, owner, id int64, key string, i RestoreInput) (*table.V22VersionRestore, error) {
	if !i.Confirmed || i.ExpectedRevision < 1 || i.VersionID == "" || key == "" || len([]rune(key)) > 128 {
		return nil, ErrInvalid
	}
	s := session(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, owner, "version.restore.capacity", "owner"); err != nil {
		return nil, err
	}
	prior := new(table.V22VersionRestore)
	has, err := s.Where("owner_id=? AND idempotency_key=?", owner, key).Get(prior)
	if err != nil {
		return nil, err
	}
	fp := hash([]any{id, i})
	if has {
		if prior.Fingerprint != fp {
			return nil, ErrConflict
		}
		return prior, nil
	}
	n, err := s.Where("owner_id=?", owner).Count(new(table.V22VersionRestore))
	if err != nil {
		return nil, err
	}
	if n >= 100 {
		return nil, ErrRestoreCapacity
	}
	if _, err = s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND owner_id=? FOR UPDATE", id, owner); err != nil {
		return nil, err
	}
	c := new(table.V22Collector)
	has, err = s.Where("id=? AND owner_id=?", id, owner).Get(c)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if c.Revision != i.ExpectedRevision {
		return nil, ErrConflict
	}
	v := new(table.V22Version)
	has, err = s.Where("id=? AND owner_id=? AND collector_id=?", i.VersionID, owner, id).Get(v)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if v.DefinitionHash != publication.DefinitionHash(v.Definition) {
		return nil, ErrConflict
	}
	definition, err := repair.ForkDefinition(v.Definition)
	if err != nil {
		return nil, ErrConflict
	}
	var root struct {
		EntryURL string `json:"entry_url"`
	}
	if json.Unmarshal([]byte(definition), &root) != nil || root.EntryURL == "" {
		return nil, ErrConflict
	}
	now := time.Now()
	draft := &table.V22Collector{OwnerId: owner, Name: repair.ForkName(v.Name), EntryURL: root.EntryURL, EntryType: v.EntryType, Status: "draft", Definition: definition, Revision: 1, ValidationSummary: `{"valid":false,"errors":[]}`, CreatedBy: owner, UpdatedBy: owner, CreatedAt: now, UpdatedAt: now}
	if _, err = s.InsertOne(draft); err != nil {
		return nil, err
	}
	r := &table.V22VersionRestore{Id: uuid.NewString(), OwnerId: owner, CollectorId: id, VersionId: v.Id, TargetCollectorId: draft.Id, IdempotencyKey: key, Fingerprint: fp, CreatedAt: now}
	if _, err = s.InsertOne(r); err != nil {
		return nil, err
	}
	return r, s.Commit()
}
func GetRestore(ctx context.Context, owner int64, key string) (*table.V22VersionRestore, error) {
	s := session(ctx)
	defer s.Close()
	r := new(table.V22VersionRestore)
	has, err := s.Where("owner_id=? AND idempotency_key=?", owner, key).Get(r)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return r, nil
}
