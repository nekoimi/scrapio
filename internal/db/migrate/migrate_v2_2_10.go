package migrate

import "xorm.io/xorm"

type v22Versions struct{}

func init()                         { registerMigrate(new(v22Versions)) }
func (*v22Versions) Version() int64 { return 2026_10_10_002 }
func (*v22Versions) Desc() string {
	return "新增 v2.2 发布检查、不可变版本与证据保留引用"
}
func (*v22Versions) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_publish_checks (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 collector_revision INTEGER NOT NULL,
 trial_id VARCHAR(36) NOT NULL, definition_hash VARCHAR(64) NOT NULL,
 manifest_hash VARCHAR(64) NOT NULL, capability_hash VARCHAR(64) NOT NULL,
 accept_limited BOOLEAN NOT NULL, ready BOOLEAN NOT NULL,
 result JSONB NOT NULL, manifest JSONB NOT NULL, capabilities JSONB NOT NULL, evidence JSONB NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL, fingerprint VARCHAR(64) NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_publish_checks_collector ON v22_publish_checks(owner_id,collector_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS v22_versions (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 number INTEGER NOT NULL, collector_revision INTEGER NOT NULL,
 name VARCHAR(160) NOT NULL, entry_type VARCHAR(16) NOT NULL,
 definition JSONB NOT NULL, definition_hash VARCHAR(64) NOT NULL,
 output_schema JSONB NOT NULL, runtime_config JSONB NOT NULL,
 check_id VARCHAR(36) NOT NULL REFERENCES v22_publish_checks(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
 trial_id VARCHAR(36) NOT NULL REFERENCES v22_trials(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
 output_check_id VARCHAR(36) NOT NULL REFERENCES v22_output_checks(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
 contract_version VARCHAR(32) NOT NULL, interpreter_version VARCHAR(32) NOT NULL,
 capability_hash VARCHAR(64) NOT NULL, manifest_hash VARCHAR(64) NOT NULL,
 note VARCHAR(1000) NOT NULL DEFAULT '', idempotency_key VARCHAR(128) NOT NULL, fingerprint VARCHAR(64) NOT NULL,
 published_by BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key), UNIQUE(collector_id,number), UNIQUE(check_id)
);
CREATE INDEX IF NOT EXISTS idx_v22_versions_collector ON v22_versions(owner_id,collector_id,number DESC);
ALTER TABLE v22_collectors ADD COLUMN IF NOT EXISTS published_version_id VARCHAR(36);
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='fk_v22_collector_published_version') THEN
  ALTER TABLE v22_collectors ADD CONSTRAINT fk_v22_collector_published_version FOREIGN KEY(published_version_id) REFERENCES v22_versions(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED;
 END IF;
END $$;
`)
	return err
}
