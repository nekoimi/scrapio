package migrate

import "xorm.io/xorm"

type v22Captures struct{}

func init()                         { registerMigrate(new(v22Captures)) }
func (*v22Captures) Version() int64 { return 2026_10_09_002 }
func (*v22Captures) Desc() string   { return "新增 v2.2 HTTP 与离线输入快照" }
func (*v22Captures) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_captures (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 draft_revision INTEGER NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 request JSONB NOT NULL,
 source VARCHAR(16) NOT NULL,
 format VARCHAR(16) NOT NULL,
 status VARCHAR(24) NOT NULL CHECK (status IN ('running','succeeded','failed','uncertain')),
 final_url TEXT NOT NULL DEFAULT '',
 content_type TEXT NOT NULL DEFAULT '',
 status_code INTEGER NOT NULL DEFAULT 0,
 content TEXT NOT NULL DEFAULT '',
 content_hash VARCHAR(64) NOT NULL DEFAULT '',
 byte_count INTEGER NOT NULL DEFAULT 0,
 error_code VARCHAR(64) NOT NULL DEFAULT '',
 error_stage VARCHAR(32) NOT NULL DEFAULT '',
 network_accessed BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 deadline_at TIMESTAMPTZ NOT NULL,
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_captures_collector ON v22_captures(owner_id,collector_id,created_at DESC);
`)
	return err
}
