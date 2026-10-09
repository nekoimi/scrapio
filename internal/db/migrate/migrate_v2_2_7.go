package migrate

import "xorm.io/xorm"

type v22Samples struct{}

func init()                        { registerMigrate(new(v22Samples)) }
func (*v22Samples) Version() int64 { return 2026_10_09_004 }
func (*v22Samples) Desc() string   { return "新增 v2.2 样例、快照保留引用与检查证据" }
func (*v22Samples) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_samples (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 capture_id VARCHAR(36) NOT NULL REFERENCES v22_captures(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
 name VARCHAR(160) NOT NULL,
 stage VARCHAR(16) NOT NULL CHECK(stage IN ('list','detail')),
 step_id VARCHAR(128) NOT NULL,
 kind VARCHAR(24) NOT NULL CHECK(kind IN ('normal','missing_field')),
 revision INTEGER NOT NULL DEFAULT 1,
 saved_revision INTEGER NOT NULL,
 definition_hash VARCHAR(64) NOT NULL,
 expected JSONB NOT NULL DEFAULT '{}',
 expected_hash VARCHAR(64) NOT NULL,
 actions JSONB NOT NULL DEFAULT '[]',
 actions_truncated BOOLEAN NOT NULL DEFAULT FALSE,
 protected BOOLEAN NOT NULL DEFAULT FALSE,
 screenshot BYTEA,
 screenshot_hash VARCHAR(64) NOT NULL DEFAULT '',
 masks JSONB NOT NULL DEFAULT '[]',
 screenshot_policy VARCHAR(64) NOT NULL DEFAULT 'excluded',
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_samples_collector ON v22_samples(owner_id,collector_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS idx_v22_samples_capture ON v22_samples(capture_id);
CREATE TABLE IF NOT EXISTS v22_sample_checks (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 sample_id VARCHAR(36) NOT NULL REFERENCES v22_samples(id) ON DELETE CASCADE,
 sample_revision INTEGER NOT NULL,
 collector_revision INTEGER NOT NULL,
 definition_hash VARCHAR(64) NOT NULL,
 definition JSONB NOT NULL,
 content_hash VARCHAR(64) NOT NULL,
 expected_hash VARCHAR(64) NOT NULL,
 expected JSONB NOT NULL,
 interpreter_version VARCHAR(64) NOT NULL,
 status VARCHAR(24) NOT NULL CHECK(status IN ('passed','failed','unconfigured')),
 result JSONB NOT NULL,
 comparison JSONB NOT NULL,
 error_code VARCHAR(64) NOT NULL DEFAULT '',
 error_message VARCHAR(512) NOT NULL DEFAULT '',
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_sample_checks_sample ON v22_sample_checks(owner_id,sample_id,created_at DESC,id DESC);
`)
	return err
}
