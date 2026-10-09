package migrate

import "xorm.io/xorm"

type v22LogicalTables struct{}

func init()                              { registerMigrate(new(v22LogicalTables)) }
func (*v22LogicalTables) Version() int64 { return 2026_10_09_005 }
func (*v22LogicalTables) Desc() string {
	return "新增 v2.2 逻辑表、输出确认与共享记录观察修订模型"
}
func (*v22LogicalTables) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_data_tables (
 id BIGSERIAL PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 name VARCHAR(160) NOT NULL,
 schema_version INTEGER NOT NULL DEFAULT 1,
 schema JSONB NOT NULL CHECK(jsonb_typeof(schema)='object'),
 schema_hash VARCHAR(64) NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_data_tables_owner ON v22_data_tables(owner_id,id DESC);
CREATE TABLE IF NOT EXISTS v22_data_table_schemas (
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE CASCADE,
 version INTEGER NOT NULL,
 schema JSONB NOT NULL,
 schema_hash VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(table_id,version)
);
CREATE TABLE IF NOT EXISTS v22_output_checks (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 collector_revision INTEGER NOT NULL,
 definition_hash VARCHAR(64) NOT NULL,
 capture_id VARCHAR(36) NOT NULL REFERENCES v22_captures(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
 content_hash VARCHAR(64) NOT NULL,
 sample_id VARCHAR(36) NOT NULL DEFAULT '',
 sample_revision INTEGER NOT NULL DEFAULT 0,
 table_id BIGINT REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 schema_version INTEGER NOT NULL,
 schema_hash VARCHAR(64) NOT NULL,
 config JSONB NOT NULL,
 result JSONB NOT NULL,
 ready BOOLEAN NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_output_checks_collector ON v22_output_checks(owner_id,collector_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS v22_output_bindings (
 collector_id BIGINT PRIMARY KEY REFERENCES v22_collectors(id) ON DELETE CASCADE,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 check_id VARCHAR(36) NOT NULL REFERENCES v22_output_checks(id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
 config JSONB NOT NULL,
 bound_revision INTEGER NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS v22_table_records (
 id VARCHAR(36) PRIMARY KEY,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 schema_version INTEGER NOT NULL,
 canonical_key VARCHAR(64) NOT NULL,
 key_values JSONB NOT NULL,
 record_values JSONB NOT NULL,
 values_hash VARCHAR(64) NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 first_observed_at TIMESTAMPTZ NOT NULL,
 last_observed_at TIMESTAMPTZ NOT NULL,
 UNIQUE(table_id,canonical_key),
 FOREIGN KEY(table_id,schema_version) REFERENCES v22_data_table_schemas(table_id,version)
);
CREATE TABLE IF NOT EXISTS v22_table_observations (
 id VARCHAR(36) PRIMARY KEY,
 record_id VARCHAR(36) NOT NULL REFERENCES v22_table_records(id) ON DELETE CASCADE,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 schema_version INTEGER NOT NULL,
 collector_id BIGINT REFERENCES v22_collectors(id) ON DELETE SET NULL,
 run_id VARCHAR(36) NOT NULL,
 version_id VARCHAR(36) NOT NULL,
 document_id VARCHAR(36) NOT NULL,
 capture_id VARCHAR(36) REFERENCES v22_captures(id) ON DELETE SET NULL,
 source_url TEXT NOT NULL,
 page_role VARCHAR(16) NOT NULL,
 observed_values JSONB NOT NULL,
 observed_values_hash VARCHAR(64) NOT NULL,
 write_key VARCHAR(128) NOT NULL,
 outcome VARCHAR(24) NOT NULL CHECK(outcome IN ('created','updated','unchanged')),
 observed_at TIMESTAMPTZ NOT NULL,
 UNIQUE(table_id,write_key),
 FOREIGN KEY(table_id,schema_version) REFERENCES v22_data_table_schemas(table_id,version)
);
CREATE INDEX IF NOT EXISTS idx_v22_table_observations_record ON v22_table_observations(record_id,observed_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS v22_table_record_revisions (
 record_id VARCHAR(36) NOT NULL REFERENCES v22_table_records(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL,
 schema_version INTEGER NOT NULL,
 record_values JSONB NOT NULL,
 values_hash VARCHAR(64) NOT NULL,
 changed_fields JSONB NOT NULL,
 observation_id VARCHAR(36) NOT NULL REFERENCES v22_table_observations(id) ON DELETE CASCADE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(record_id,revision)
);
`)
	return err
}
