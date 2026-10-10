package migrate

import "xorm.io/xorm"

type v22DataWork struct{}

func init()                         { registerMigrate(new(v22DataWork)) }
func (*v22DataWork) Version() int64 { return 2026_10_10_007 }
func (*v22DataWork) Desc() string {
	return "v2.2 冻结查询快照、保存视图和有界异步导出"
}
func (*v22DataWork) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_data_snapshots (
 id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 query JSONB NOT NULL,query_hash VARCHAR(64) NOT NULL,schema JSONB NOT NULL,schema_version INTEGER NOT NULL,
 rows JSONB NOT NULL,count INTEGER NOT NULL,captured_at TIMESTAMPTZ NOT NULL,expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_v22_data_snapshots_owner ON v22_data_snapshots(owner_id,expires_at);
CREATE TABLE IF NOT EXISTS v22_data_views (
 id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 name VARCHAR(80) NOT NULL,revision INTEGER NOT NULL,query JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_v22_data_views_table ON v22_data_views(owner_id,table_id);
CREATE TABLE IF NOT EXISTS v22_exports (
 id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 snapshot_id VARCHAR(36) REFERENCES v22_data_snapshots(id) ON DELETE RESTRICT,
 format VARCHAR(8) NOT NULL CHECK(format IN ('csv','json')),
 status VARCHAR(16) NOT NULL CHECK(status IN ('queued','running','succeeded','failed','cancelled','expired')),
 progress INTEGER NOT NULL DEFAULT 0,row_count INTEGER NOT NULL,query JSONB NOT NULL,schema JSONB NOT NULL,
 captured_at TIMESTAMPTZ NOT NULL,idempotency_key VARCHAR(128) NOT NULL,fingerprint VARCHAR(64) NOT NULL,
 lease_token VARCHAR(36) NOT NULL DEFAULT '',lease_until TIMESTAMPTZ,
 error_code VARCHAR(64) NOT NULL DEFAULT '',file BYTEA,file_hash VARCHAR(64) NOT NULL DEFAULT '',file_bytes INTEGER NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL,finished_at TIMESTAMPTZ,expires_at TIMESTAMPTZ NOT NULL,
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_exports_queue ON v22_exports(created_at) WHERE status='queued';
CREATE INDEX IF NOT EXISTS idx_v22_exports_owner ON v22_exports(owner_id,created_at DESC,id DESC);
`)
	return err
}
