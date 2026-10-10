package v22_schedule_repo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_run_repo"
	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/schedule"
	"github.com/nekoimi/scrapio/internal/trial"
)

var ErrAuth = errors.New("invalid, revoked or out-of-scope API credential")

func stringID(id int64) string { return strconv.FormatInt(id, 10) }
func TokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func Bearer(header string) (string, error) {
	if !strings.HasPrefix(header, "Bearer sap_") || len(header) != len("Bearer sap_")+64 {
		return "", ErrAuth
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if _, err := hex.DecodeString(strings.TrimPrefix(token, "sap_")); err != nil {
		return "", ErrAuth
	}
	return token, nil
}
func Keys(ctx context.Context, owner, id int64) ([]table.V22APIKey, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := collector(s, owner, id); err != nil {
		return nil, err
	}
	rows := []table.V22APIKey{}
	err := s.Where("owner_id=? AND collector_id=?", owner, id).Omit("token_hash").Desc("created_at").Limit(100).Find(&rows)
	return rows, err
}
func CreateKey(ctx context.Context, owner, id int64, keyID, name string, input runmodel.Input) (*table.V22APIKey, string, error) {
	if _, err := uuid.Parse(keyID); err != nil {
		return nil, "", invalid(errors.New("credential ID must be UUID"))
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return nil, "", invalid(errors.New("credential name requires 1..80 characters"))
	}
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, "", err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, owner, "api.keys", stringID(id)); err != nil {
		return nil, "", err
	}
	prior := new(table.V22APIKey)
	has, err := s.ID(keyID).Get(prior)
	if err != nil {
		return nil, "", err
	}
	if has {
		return nil, "", v22_run_repo.ErrConflict
	}
	if err = ValidateBinding(s, owner, id, input); err != nil {
		return nil, "", err
	}
	count, err := s.Where("owner_id=? AND collector_id=? AND revoked_at IS NULL", owner, id).Count(new(table.V22APIKey))
	if err != nil {
		return nil, "", err
	}
	total, err := s.Where("owner_id=? AND collector_id=?", owner, id).Count(new(table.V22APIKey))
	if err != nil {
		return nil, "", err
	}
	if count >= 5 || total >= 100 {
		return nil, "", invalid(errors.New("credential capacity: 5 active, 100 total per collector"))
	}
	bytes := make([]byte, 32)
	if _, err = rand.Read(bytes); err != nil {
		return nil, "", err
	}
	token := "sap_" + hex.EncodeToString(bytes)
	raw, _ := json.Marshal(input)
	row := &table.V22APIKey{Id: keyID, OwnerId: owner, CollectorId: id, Name: name, TokenHash: TokenHash(token), Input: string(raw), CreatedAt: time.Now()}
	if _, err = s.InsertOne(row); err != nil {
		return nil, "", err
	}
	if err = s.Commit(); err != nil {
		return nil, "", err
	}
	row.TokenHash = ""
	return row, token, nil
}
func Revoke(ctx context.Context, owner, id int64, keyID string) error {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	if _, err := s.QueryString("SELECT id FROM v22_api_keys WHERE owner_id=? AND collector_id=? AND id=? FOR UPDATE", owner, id, keyID); err != nil {
		return err
	}
	if err := collector(s, owner, id); err != nil {
		return err
	}
	row := new(table.V22APIKey)
	has, err := s.Where("owner_id=? AND collector_id=? AND id=?", owner, id, keyID).Get(row)
	if err != nil {
		return err
	}
	if !has {
		return v22_run_repo.ErrNotFound
	}
	if _, err = s.Exec("UPDATE v22_api_keys SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE id=?", keyID); err != nil {
		return err
	}
	return s.Commit()
}

// Trigger authenticates under a shared row lock. Revocation takes an exclusive lock,
// so no run can sneak in after a completed revocation. Caller-supplied keys are namespaced.
func Trigger(ctx context.Context, collectorID int64, token, key string, budget *trial.Budget) (*table.V22Run, error) {
	if key == "" || len([]rune(key)) > 80 {
		return nil, invalid(errors.New("Idempotency-Key requires 1..80 characters"))
	}
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	hash := TokenHash(token)
	if _, err := s.QueryString("SELECT id FROM v22_api_keys WHERE token_hash=? AND collector_id=? AND revoked_at IS NULL FOR SHARE", hash, collectorID); err != nil {
		return nil, err
	}
	cred := new(table.V22APIKey)
	has, err := s.Where("token_hash=? AND collector_id=? AND revoked_at IS NULL", hash, collectorID).Get(cred)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrAuth
	}
	var input runmodel.Input
	if err = runmodel.Decode(cred.Input, &input); err != nil {
		return nil, err
	}
	input, err = schedule.Reduce(input, budget)
	if err != nil {
		return nil, invalid(err)
	}
	row, err := v22_run_repo.CreateTx(s, cred.OwnerId, collectorID, "api:"+cred.Id+":"+key, input, nil, "api", "queue")
	if err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
