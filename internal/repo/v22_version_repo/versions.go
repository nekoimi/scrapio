package v22_version_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
)

var ErrNotFound = errors.New("publication evidence or collector not found")
var ErrConflict = errors.New("publication check expired or inputs changed")
var ErrBlocked = errors.New("publication check has blocking issues")
var ErrInvalid = errors.New("invalid publication configuration")

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func validateKey(key string) error {
	if key == "" || len([]rune(key)) > 128 {
		return invalid("Idempotency-Key required, maximum 128 characters")
	}
	return nil
}

type CheckInput struct {
	ExpectedRevision int    `json:"expected_revision"`
	TrialID          string `json:"trial_id"`
	AcceptLimited    bool   `json:"accept_limited"`
}

func (i CheckInput) Validate() error {
	if i.ExpectedRevision < 1 {
		return invalid("expected_revision required")
	}
	if _, err := uuid.Parse(i.TrialID); err != nil {
		return invalid("valid trial_id required")
	}
	return nil
}

type PublishInput struct {
	ExpectedRevision int    `json:"expected_revision"`
	CheckID          string `json:"check_id"`
	Note             string `json:"note"`
}

func (i *PublishInput) Validate() error {
	i.Note = strings.TrimSpace(i.Note)
	if i.ExpectedRevision < 1 || len([]rune(i.Note)) > 1000 {
		return invalid("revision and note up to 1000 characters required")
	}
	if _, err := uuid.Parse(i.CheckID); err != nil {
		return invalid("valid check_id required")
	}
	return nil
}
func CreateCheck(ctx context.Context, ownerID, collectorID int64, key string, input CheckInput, caps publication.Capabilities) (*table.V22PublishCheck, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	fingerprint := publication.Hash([]any{collectorID, input})
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "publication.check", key); err != nil {
		return nil, err
	}
	prior := new(table.V22PublishCheck)
	has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Fingerprint != fingerprint {
			return nil, ErrConflict
		}
		return prior, nil
	}
	st, err := load(s, ownerID, collectorID, input.ExpectedRevision, input.TrialID)
	if err != nil {
		return nil, err
	}
	result, proofs, err := evaluate(ctx, st, caps, input.AcceptLimited)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	manifestRaw, _ := json.Marshal(st.manifest)
	capRaw, _ := json.Marshal(caps)
	resultRaw, _ := json.Marshal(result)
	if len(resultRaw) > 512*1024 {
		return nil, invalid("publication diagnostics exceed 512 KiB; reduce samples or fix repeated differences")
	}
	evidence, _ := json.Marshal(proofs)
	now := time.Now()
	row := &table.V22PublishCheck{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collectorID, CollectorRevision: st.collector.Revision, TrialId: input.TrialID, DefinitionHash: st.manifest.DefinitionHash, ManifestHash: publication.Hash(st.manifest), CapabilityHash: publication.Hash(caps), AcceptLimited: input.AcceptLimited, Ready: result.Ready, Result: string(resultRaw), Manifest: string(manifestRaw), Capabilities: string(capRaw), Evidence: string(evidence), IdempotencyKey: key, Fingerprint: fingerprint, ExpiresAt: now.Add(15 * time.Minute), CreatedAt: now}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err = s.Exec("DELETE FROM v22_publish_checks WHERE owner_id=? AND collector_id=? AND id NOT IN (SELECT check_id FROM v22_versions WHERE collector_id=?) AND id NOT IN (SELECT id FROM v22_publish_checks WHERE owner_id=? AND collector_id=? ORDER BY created_at DESC,id DESC LIMIT 20)", ownerID, collectorID, collectorID, ownerID, collectorID); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func GetCheck(ownerID, collectorID int64, id, key string) (*table.V22PublishCheck, error) {
	row := new(table.V22PublishCheck)
	s := db.Instance().Where("owner_id=? AND collector_id=?", ownerID, collectorID)
	if key != "" {
		if err := validateKey(key); err != nil {
			return nil, err
		}
		s.And("idempotency_key=?", key)
	} else {
		s.And("id=?", id)
	}
	has, err := s.Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
func Publish(ctx context.Context, ownerID, collectorID int64, key string, input PublishInput, caps publication.Capabilities) (*table.V22Version, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	fingerprint := publication.Hash([]any{collectorID, input})
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "publication.version", key); err != nil {
		return nil, err
	}
	prior := new(table.V22Version)
	has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Fingerprint != fingerprint {
			return nil, ErrConflict
		}
		return prior, nil
	}
	check := new(table.V22PublishCheck)
	has, err = s.Where("owner_id=? AND collector_id=? AND id=?", ownerID, collectorID, input.CheckID).Get(check)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if !check.Ready {
		return nil, ErrBlocked
	}
	if !check.ExpiresAt.After(time.Now()) || check.CollectorRevision != input.ExpectedRevision || check.CapabilityHash != publication.Hash(caps) {
		return nil, ErrConflict
	}
	st, err := load(s, ownerID, collectorID, input.ExpectedRevision, check.TrialId)
	if err != nil {
		return nil, err
	}
	if check.ManifestHash != publication.Hash(st.manifest) || check.DefinitionHash != st.manifest.DefinitionHash {
		return nil, ErrConflict
	}
	if caps.DefinitionHash != st.manifest.DefinitionHash {
		return nil, ErrConflict
	}
	current := publication.CheckCapabilities(st.plan, caps)
	publication.Merge(&current, publication.CheckTrial(st.trial.Status, st.trial.CollectorRevision, st.trial.DefinitionHash, st.trial.FinishedAt, st.input, st.summary, input.ExpectedRevision, st.manifest.DefinitionHash, check.AcceptLimited, time.Now()))
	if !current.Ready {
		return nil, ErrBlocked
	}
	// The server evidence is self-contained; edits/deletion of samples,
	// trial results or schema must match the freshly loaded manifest above.
	var savedManifest manifest
	var savedCaps publication.Capabilities
	if decode([]byte(check.Manifest), &savedManifest) != nil || json.Unmarshal([]byte(check.Capabilities), &savedCaps) != nil || publication.Hash(savedManifest) != check.ManifestHash || publication.Hash(savedCaps) != check.CapabilityHash {
		return nil, ErrConflict
	}
	// A different key for the same confirmed check returns the one immutable version.
	has, err = s.Where("check_id=? AND owner_id=? AND collector_id=?", check.Id, ownerID, collectorID).Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Note != input.Note {
			return nil, ErrConflict
		}
		return prior, nil
	}
	count, err := s.Where("collector_id=?", collectorID).Count(new(table.V22Version))
	if err != nil {
		return nil, err
	}
	if count >= 100 {
		return nil, invalid("version limit (100) reached")
	}
	var maxRows []map[string]string
	maxRows, err = s.QueryString("SELECT COALESCE(MAX(number),0) AS number FROM v22_versions WHERE collector_id=?", collectorID)
	if err != nil {
		return nil, err
	}
	number := int(count) + 1
	if len(maxRows) > 0 {
		var n int
		_, _ = fmt.Sscan(maxRows[0]["number"], &n)
		number = n + 1
	}
	runtimeRaw, _ := json.Marshal(st.input)
	row := &table.V22Version{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collectorID, Number: number, CollectorRevision: st.collector.Revision, Name: st.collector.Name, EntryType: st.collector.EntryType, Definition: st.collector.Definition, DefinitionHash: st.manifest.DefinitionHash, OutputSchema: st.schemaRaw, RuntimeConfig: string(runtimeRaw), CheckId: check.Id, TrialId: check.TrialId, OutputCheckId: st.plan.Output.CheckID, ContractVersion: publication.ContractVersion, InterpreterVersion: extraction.InterpreterVersion, CapabilityHash: check.CapabilityHash, ManifestHash: check.ManifestHash, Note: input.Note, IdempotencyKey: key, Fingerprint: fingerprint, PublishedBy: ownerID, CreatedAt: time.Now()}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err = s.Exec("UPDATE v22_collectors SET published_version_id=? WHERE id=? AND owner_id=?", row.Id, collectorID, ownerID); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Get(ownerID int64, id, key string) (*table.V22Version, error) {
	row := new(table.V22Version)
	s := db.Instance().Where("owner_id=?", ownerID)
	if key != "" {
		if err := validateKey(key); err != nil {
			return nil, err
		}
		s.And("idempotency_key=?", key)
	} else {
		s.And("id=?", id)
	}
	has, err := s.Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
func List(ownerID, collectorID int64, before, limit int) ([]table.V22Version, bool, error) {
	if before < 0 || limit < 1 || limit > 50 {
		return nil, false, invalid("invalid pagination")
	}
	rows := []table.V22Version{}
	s := db.Instance().Where("owner_id=? AND collector_id=?", ownerID, collectorID)
	if before > 0 {
		s.And("number<?", before)
	}
	err := s.Omit("definition", "output_schema", "runtime_config").Desc("number").Limit(limit + 1).Find(&rows)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return rows, more, nil
}
