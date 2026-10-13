package migrate

import "xorm.io/xorm"

type coCollectors struct{}

func init()                          { registerMigrate(new(coCollectors)) }
func (*coCollectors) Version() int64 { return 2026_10_13_001 }
func (*coCollectors) Desc() string {
	return "independent collector.v1 domain, immutable versions and bounded test evidence"
}
func (*coCollectors) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 CREATE TABLE IF NOT EXISTS co_collectors (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id), name VARCHAR(64) NOT NULL,
 revision INT NOT NULL DEFAULT 0, definition JSONB NOT NULL DEFAULT '{}', archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,id));
 CREATE INDEX IF NOT EXISTS idx_co_collectors_owner ON co_collectors(owner_id,created_at DESC,id);
 CREATE TABLE IF NOT EXISTS co_operations (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id), resource_id VARCHAR(36) NOT NULL,
 operation TEXT NOT NULL, key VARCHAR(128) NOT NULL, fingerprint TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','failed','cancelled')),
 payload JSONB NOT NULL DEFAULT '{}', result JSONB NOT NULL DEFAULT '{}', error JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ,
 UNIQUE(owner_id,operation,resource_id,key));
 CREATE INDEX IF NOT EXISTS idx_co_operations_pending ON co_operations(created_at) WHERE status='queued';
 CREATE INDEX IF NOT EXISTS idx_co_operations_resource ON co_operations(owner_id,resource_id,created_at DESC);
 CREATE TABLE IF NOT EXISTS co_samples (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL, collector_id VARCHAR(36) NOT NULL,
 revision INT NOT NULL, input JSONB NOT NULL, snapshots JSONB NOT NULL, expected_outputs JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), FOREIGN KEY(owner_id,collector_id) REFERENCES co_collectors(owner_id,id));
 CREATE INDEX IF NOT EXISTS idx_co_samples_collector ON co_samples(owner_id,collector_id,created_at DESC);
 CREATE TABLE IF NOT EXISTS co_collector_versions (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL, collector_id VARCHAR(36) NOT NULL,
 number INT NOT NULL, revision INT NOT NULL, definition JSONB NOT NULL, definition_hash TEXT NOT NULL,
 input_schema_hash TEXT NOT NULL, output_schema_hash TEXT NOT NULL,
 interpreter_version TEXT NOT NULL, publish_version TEXT NOT NULL, requirements JSONB NOT NULL,
 evidence_id VARCHAR(36) NOT NULL REFERENCES co_operations(id), check_id VARCHAR(36) NOT NULL REFERENCES co_operations(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), FOREIGN KEY(owner_id,collector_id) REFERENCES co_collectors(owner_id,id),
 UNIQUE(collector_id,number), UNIQUE(collector_id,revision));
 CREATE TABLE IF NOT EXISTS co_sessions (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL, collector_id VARCHAR(36) NOT NULL, revision INT NOT NULL,
 definition JSONB NOT NULL, input JSONB NOT NULL, expires_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(owner_id,collector_id) REFERENCES co_collectors(owner_id,id));
 `)
	return err
}
