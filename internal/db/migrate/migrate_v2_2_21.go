package migrate

import "xorm.io/xorm"

type v22Governance struct{}

func init()                           { registerMigrate(new(v22Governance)) }
func (*v22Governance) Version() int64 { return 2026_10_10_013 }
func (*v22Governance) Desc() string {
	return "v2.2 Schema impact checks, asset retention and owner operation audit"
}
func (*v22Governance) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 CREATE INDEX IF NOT EXISTS idx_v22_output_checks_capture ON v22_output_checks(capture_id);
 CREATE INDEX IF NOT EXISTS idx_v22_observations_capture ON v22_table_observations(capture_id) WHERE capture_id IS NOT NULL;
 CREATE INDEX IF NOT EXISTS idx_v22_repairs_capture ON v22_repair_drafts(capture_id) WHERE capture_id IS NOT NULL;
 CREATE TABLE IF NOT EXISTS v22_trial_capture_pins (
 trial_id VARCHAR(36) NOT NULL REFERENCES v22_trials(id) ON DELETE CASCADE,
 capture_id VARCHAR(36) NOT NULL REFERENCES v22_captures(id) ON DELETE RESTRICT,
 PRIMARY KEY(trial_id,capture_id));
 CREATE INDEX IF NOT EXISTS idx_v22_trial_capture_pins_capture ON v22_trial_capture_pins(capture_id);
 INSERT INTO v22_trial_capture_pins(trial_id,capture_id)
 SELECT DISTINCT t.id,c.id FROM v22_trials t CROSS JOIN LATERAL jsonb_each(t.fixed_inputs) f
 JOIN v22_captures c ON c.id=(f.value->>'capture_id') AND c.owner_id=t.owner_id
 ON CONFLICT DO NOTHING;
 CREATE TABLE IF NOT EXISTS v22_schema_checks (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 table_id BIGINT NOT NULL REFERENCES v22_data_tables(id) ON DELETE RESTRICT,
 expected_version INT NOT NULL, schema JSONB NOT NULL, impact JSONB NOT NULL, dependency_hash TEXT NOT NULL,
 applied_version INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), expires_at TIMESTAMPTZ NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_v22_schema_checks_owner ON v22_schema_checks(owner_id,table_id,created_at);
 CREATE TABLE IF NOT EXISTS v22_retention_settings (
 owner_id BIGINT PRIMARY KEY REFERENCES admin(id) ON DELETE RESTRICT,
 revision INT NOT NULL, capture_days INT NOT NULL CHECK(capture_days BETWEEN 1 AND 365),
 asset_limit_mib INT NOT NULL CHECK(asset_limit_mib BETWEEN 16 AND 4096), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE TABLE IF NOT EXISTS v22_cleanup_checks (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 policy_revision INT NOT NULL, candidates JSONB NOT NULL, result JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), expires_at TIMESTAMPTZ NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_v22_cleanup_checks_owner ON v22_cleanup_checks(owner_id,created_at);
 CREATE TABLE IF NOT EXISTS v22_operation_logs (
 id BIGSERIAL PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 actor_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 request_id TEXT NOT NULL DEFAULT '', action TEXT NOT NULL, resource_id TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL, http_status INT NOT NULL DEFAULT 0, details JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ);
 CREATE INDEX IF NOT EXISTS idx_v22_operation_logs_owner ON v22_operation_logs(owner_id,id DESC);
 `)
	return err
}
