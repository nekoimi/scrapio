package migrate

import "xorm.io/xorm"

type v22CollectorDrafts struct{}

func init() { registerMigrate(new(v22CollectorDrafts)) }

func (*v22CollectorDrafts) Version() int64 { return 2026_09_29_001 }
func (*v22CollectorDrafts) Desc() string {
	return "新增 v2.2 独立采集方案与草稿幂等模型"
}

func (*v22CollectorDrafts) Exec(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	_, err := s.Exec(`
CREATE TABLE IF NOT EXISTS v22_collectors (
 id BIGSERIAL PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 name VARCHAR(160) NOT NULL,
 entry_url TEXT NOT NULL,
 entry_type VARCHAR(16) NOT NULL CHECK (entry_type IN ('web','json')),
 status VARCHAR(24) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
 definition JSONB NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
 created_by BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 updated_by BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 archived_at TIMESTAMPTZ,
 CONSTRAINT v22_collectors_definition_object CHECK (jsonb_typeof(definition) = 'object')
);
CREATE INDEX IF NOT EXISTS idx_v22_collectors_owner_updated ON v22_collectors(owner_id, updated_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS v22_idempotency_keys (
 id BIGSERIAL PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 operation VARCHAR(64) NOT NULL,
 key VARCHAR(128) NOT NULL,
 resource_id BIGINT NOT NULL,
 response JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE (owner_id, operation, key)
);
`)
	if err != nil {
		return err
	}
	return s.Commit()
}
