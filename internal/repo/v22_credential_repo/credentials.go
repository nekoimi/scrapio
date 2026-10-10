package v22_credential_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/credential"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"os"
	"time"
	"xorm.io/xorm"
	xormlog "xorm.io/xorm/log"
)

var ErrNotFound = errors.New("credential not found")
var ErrConflict = errors.New("credential revision or request changed")

func session(ctx context.Context) *xorm.Session {
	return db.Instance().NewSession().Context(context.WithValue(ctx, xormlog.SessionShowSQLKey, false))
}
func get(s *xorm.Session, owner int64, id string) (*table.V22Credential, error) {
	r := new(table.V22Credential)
	has, e := s.Where("owner_id=? AND id=?", owner, id).Get(r)
	if e != nil {
		return nil, e
	}
	if !has {
		return nil, ErrNotFound
	}
	return r, nil
}
func aad(r *table.V22Credential) string {
	return fmt.Sprintf("%d:%s:%s:%s", r.OwnerId, r.Id, r.Kind, r.Origin)
}
func metadata(r *table.V22Credential) map[string]any {
	var masks []string
	_ = json.Unmarshal([]byte(r.MaskSelectors), &masks)
	status := "available"
	if r.RevokedAt != nil {
		status = "revoked"
	} else if !r.ExpiresAt.After(time.Now()) {
		status = "expired"
	} else if r.Storage == "encrypted" && (!credential.KeyReady() || r.KeyId != credential.KeyID()) {
		status = "key_unavailable"
	} else if r.Storage == "environment" && os.Getenv(r.Env) == "" {
		status = "environment_missing"
	}
	return map[string]any{"credential_id": r.Id, "credential_ref": credential.Ref(r.Id), "revision": r.Revision, "name": r.Name, "kind": r.Kind, "origin": r.Origin, "storage": r.Storage, "header": r.Header, "prefix": r.Prefix, "env": r.Env, "expires_at": r.ExpiresAt, "mask_selectors": masks, "status": status, "revoked_at": r.RevokedAt, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt}
}
func Get(ctx context.Context, owner int64, id string) (any, error) {
	s := session(ctx)
	defer s.Close()
	r, e := get(s, owner, id)
	if e != nil {
		return nil, e
	}
	return metadata(r), nil
}
func List(ctx context.Context, owner int64) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows := []table.V22Credential{}
	if e := s.Where("owner_id=?", owner).Desc("created_at", "id").Limit(100).Find(&rows); e != nil {
		return nil, e
	}
	items := []any{}
	for _, r := range rows {
		items = append(items, metadata(&r))
	}
	return map[string]any{"items": items, "encryption_ready": credential.KeyReady(), "limit": 100}, nil
}
func Save(ctx context.Context, owner int64, i credential.Input, create bool) (any, error) {
	if id, e := uuid.Parse(i.ID); e != nil || id.String() != i.ID {
		return nil, credential.ErrInvalid
	}
	if create && i.ExpectedRevision != 0 {
		return nil, credential.ErrInvalid
	}
	if e := i.Validate(time.Now(), create); e != nil {
		return nil, e
	}
	finger, e := credential.Fingerprint(i)
	if e != nil {
		return nil, e
	}
	s := session(ctx)
	defer s.Close()
	if e = s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	if e = idempotency.Lock(s, owner, "credential.save", "owner"); e != nil {
		return nil, e
	}
	r, e := get(s, owner, i.ID)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	if r != nil {
		if create {
			if r.CreateFingerprint == finger {
				return metadata(r), nil
			}
			return nil, ErrConflict
		}
		if r.RevokedAt != nil {
			return nil, ErrConflict
		}
		if r.Revision != i.ExpectedRevision {
			if r.Revision == i.ExpectedRevision+1 && r.Fingerprint == finger {
				return metadata(r), nil
			}
			return nil, ErrConflict
		}
	} else {
		if !create || i.ExpectedRevision != 0 {
			return nil, ErrNotFound
		}
		n, err := s.Where("owner_id=?", owner).Count(new(table.V22Credential))
		if err != nil {
			return nil, err
		}
		if n >= 100 {
			return nil, ErrConflict
		}
		r = &table.V22Credential{Id: i.ID, OwnerId: owner, CreatedAt: time.Now(), CreateFingerprint: finger}
	}
	secret := i.Secret
	if i.Storage == "encrypted" && secret == "" {
		if r.Storage != "encrypted" || r.Kind != i.Kind || r.Origin != i.Origin {
			return nil, credential.ErrInvalid
		}
		secret, e = credential.Open(r.Ciphertext, aad(r))
		if e != nil {
			return nil, e
		}
	}
	r.Name = i.Name
	r.Kind = i.Kind
	r.Origin = i.Origin
	r.Storage = i.Storage
	r.Header = i.Header
	r.Prefix = i.Prefix
	r.Env = i.Env
	r.ExpiresAt = i.ExpiresAt
	r.Revision++
	r.UpdatedAt = time.Now()
	m, _ := json.Marshal(i.MaskSelectors)
	if i.MaskSelectors == nil {
		m = []byte("[]")
	}
	r.MaskSelectors = string(m)
	r.Fingerprint = finger
	r.Ciphertext = ""
	r.KeyId = ""
	if i.Storage == "encrypted" {
		if e = credential.ValidateSecret(i.Kind, secret); e != nil {
			return nil, e
		}
		r.Ciphertext, e = credential.Seal(secret, aad(r))
		if e != nil {
			return nil, e
		}
		r.KeyId = credential.KeyID()
	}
	if create {
		_, e = s.InsertOne(r)
	} else {
		_, e = s.ID(r.Id).AllCols().Update(r)
	}
	if e != nil {
		return nil, e
	}
	if _, e = s.Exec("UPDATE v22_browser_sessions SET status='expired',closed_at=NOW(),updated_at=NOW() WHERE owner_id=? AND credential_ref=? AND credential_revision<>? AND status IN ('creating','ready','disconnected')", owner, credential.Ref(r.Id), r.Revision); e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return metadata(r), nil
}
func Revoke(ctx context.Context, owner int64, id string, revision int) (any, error) {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	if e := idempotency.Lock(s, owner, "credential.save", "owner"); e != nil {
		return nil, e
	}
	r, e := get(s, owner, id)
	if e != nil {
		return nil, e
	}
	if r.RevokedAt != nil {
		return metadata(r), nil
	}
	if r.Revision != revision {
		return nil, ErrConflict
	}
	now := time.Now()
	r.RevokedAt = &now
	r.UpdatedAt = now
	r.Revision++
	r.Ciphertext = ""
	if _, e = s.ID(id).Cols("revoked_at", "updated_at", "revision", "ciphertext").Update(r); e != nil {
		return nil, e
	}
	if _, e = s.Exec("UPDATE v22_browser_sessions SET status='expired',closed_at=NOW(),updated_at=NOW() WHERE owner_id=? AND credential_ref=? AND status IN ('creating','ready','disconnected')", owner, credential.Ref(id)); e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return metadata(r), nil
}
func Resolve(ctx context.Context, owner int64, target, ref, kind string) (*table.V22Credential, string, error) {
	id := credential.ID(ref)
	if id == "" {
		return nil, "", credential.ErrUnavailable
	}
	s := session(ctx)
	defer s.Close()
	r, e := get(s, owner, id)
	if e != nil {
		return nil, "", credential.ErrUnavailable
	}
	o, e := credential.Origin(target)
	if e != nil || o != r.Origin || r.Kind != kind || r.RevokedAt != nil || !r.ExpiresAt.After(time.Now()) {
		return nil, "", credential.ErrUnavailable
	}
	secret := ""
	if r.Storage == "environment" {
		secret = os.Getenv(r.Env)
	} else {
		secret, e = credential.Open(r.Ciphertext, aad(r))
	}
	if e != nil || credential.ValidateSecret(kind, secret) != nil {
		return nil, "", credential.ErrUnavailable
	}
	return r, secret, nil
}
func HTTP(ctx context.Context, owner int64, target, ref string) (string, string, string, error) {
	r, secret, e := Resolve(ctx, owner, target, ref, "http_header")
	if e != nil {
		return "", "", "", e
	}
	return r.Header, r.Prefix + secret, secret, nil
}
func Browser(ctx context.Context, owner int64, target, ref string) (credential.Authorization, int, error) {
	if ref == "" {
		return credential.Authorization{}, 0, nil
	}
	r, secret, e := Resolve(ctx, owner, target, ref, "browser_cookie")
	if e != nil {
		return credential.Authorization{}, 0, e
	}
	cookies, e := credential.Cookies(secret)
	var masks []string
	_ = json.Unmarshal([]byte(r.MaskSelectors), &masks)
	return credential.Authorization{Origin: r.Origin, Cookies: cookies, MaskSelectors: masks, ExpiresAt: r.ExpiresAt}, r.Revision, e
}
