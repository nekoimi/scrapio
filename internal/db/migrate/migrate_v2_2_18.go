package migrate

import "xorm.io/xorm"

type v22Regressions struct{}

func init()                            { registerMigrate(new(v22Regressions)) }
func (*v22Regressions) Version() int64 { return 2026_10_10_010 }
func (*v22Regressions) Desc() string {
	return "v2.2 冻结样例回归、版本比较与历史草稿复制"
}
func (*v22Regressions) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`CREATE TABLE IF NOT EXISTS v22_regressions (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 kind VARCHAR(20) NOT NULL CHECK(kind IN ('regression','comparison')),
 collector_revision INT NOT NULL,
 base_version_id VARCHAR(36) NOT NULL DEFAULT '', target_version_id VARCHAR(36) NOT NULL DEFAULT '',
 base_hash VARCHAR(64) NOT NULL, target_hash VARCHAR(64) NOT NULL,
 snapshot JSONB NOT NULL, snapshot_hash VARCHAR(64) NOT NULL,
 status VARCHAR(20) NOT NULL CHECK(status IN ('queued','running','succeeded','failed','cancelled')),
 total INT NOT NULL CHECK(total BETWEEN 0 AND 100), completed INT NOT NULL DEFAULT 0 CHECK(completed BETWEEN 0 AND total),
 report JSONB NOT NULL DEFAULT '{}', error_code VARCHAR(64) NOT NULL DEFAULT '', lease VARCHAR(36) NOT NULL DEFAULT '',
 deadline_at TIMESTAMPTZ NOT NULL, idempotency_key VARCHAR(128) NOT NULL, fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ,
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_regressions_queue ON v22_regressions(status,created_at);
CREATE INDEX IF NOT EXISTS idx_v22_regressions_owner ON v22_regressions(owner_id,collector_id,created_at DESC,id);
ALTER TABLE v22_versions ADD COLUMN IF NOT EXISTS difference_review JSONB NOT NULL DEFAULT '{}';
CREATE TABLE IF NOT EXISTS v22_version_restores (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 version_id VARCHAR(36) NOT NULL REFERENCES v22_versions(id) ON DELETE RESTRICT,
 target_collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 idempotency_key VARCHAR(128) NOT NULL, fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(owner_id,idempotency_key)
);`)
	return err
}
