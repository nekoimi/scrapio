package migrate

import "xorm.io/xorm"

type v22Runs struct{}

func init()                     { registerMigrate(new(v22Runs)) }
func (*v22Runs) Version() int64 { return 2026_10_10_003 }
func (*v22Runs) Desc() string {
	return "新增 v2.2 正式运行、租约、文档事件及记录追溯外键"
}
func (*v22Runs) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_runs (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 version_id VARCHAR(36) NOT NULL REFERENCES v22_versions(id) ON DELETE RESTRICT,
 version_number INTEGER NOT NULL, collector_revision INTEGER NOT NULL,
 definition JSONB NOT NULL, definition_hash VARCHAR(64) NOT NULL,
 publication_contract VARCHAR(32) NOT NULL, interpreter_version VARCHAR(32) NOT NULL,
 entry_type VARCHAR(16) NOT NULL, output_schema JSONB NOT NULL, input JSONB NOT NULL,
 trigger_source VARCHAR(24) NOT NULL CHECK(trigger_source='manual'),
 status VARCHAR(24) NOT NULL CHECK(status IN ('queued','running','succeeded','limited','partial','failed','cancelled')),
 attempt INTEGER NOT NULL DEFAULT 0, lease_token VARCHAR(36) NOT NULL DEFAULT '', lease_until TIMESTAMPTZ,
 cancel_requested BOOLEAN NOT NULL DEFAULT FALSE, session_id VARCHAR(36) NOT NULL DEFAULT '',
 current_step VARCHAR(128) NOT NULL DEFAULT '', current_stage VARCHAR(32) NOT NULL DEFAULT '',
 event_seq BIGINT NOT NULL, summary JSONB NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL, fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
 UNIQUE(owner_id,idempotency_key), UNIQUE(id,version_id)
);
CREATE INDEX IF NOT EXISTS idx_v22_runs_owner ON v22_runs(owner_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS idx_v22_runs_queue ON v22_runs(created_at,id) WHERE status='queued';
CREATE INDEX IF NOT EXISTS idx_v22_runs_lease ON v22_runs(lease_until) WHERE status='running';
CREATE TABLE IF NOT EXISTS v22_run_attempts (
 run_id VARCHAR(36) NOT NULL REFERENCES v22_runs(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL, lease_token VARCHAR(36) NOT NULL,
 status VARCHAR(24) NOT NULL, session_id VARCHAR(36) NOT NULL DEFAULT '',
 summary JSONB NOT NULL DEFAULT '{}', started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ,
 PRIMARY KEY(run_id,attempt)
);
CREATE TABLE IF NOT EXISTS v22_run_documents (
 id VARCHAR(36) PRIMARY KEY, run_id VARCHAR(36) NOT NULL REFERENCES v22_runs(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL, sequence BIGINT NOT NULL, content TEXT NOT NULL, result JSONB NOT NULL,
 UNIQUE(run_id,sequence), UNIQUE(id,run_id),
 FOREIGN KEY(run_id,attempt) REFERENCES v22_run_attempts(run_id,attempt)
);
CREATE TABLE IF NOT EXISTS v22_run_events (
 run_id VARCHAR(36) NOT NULL REFERENCES v22_runs(id) ON DELETE CASCADE,
 sequence BIGINT NOT NULL, attempt INTEGER NOT NULL, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(run_id,sequence)
);
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='fk_v22_observation_run_version') THEN
  ALTER TABLE v22_table_observations ADD CONSTRAINT fk_v22_observation_run_version FOREIGN KEY(run_id,version_id) REFERENCES v22_runs(id,version_id) ON DELETE RESTRICT;
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='fk_v22_observation_document_run') THEN
  ALTER TABLE v22_table_observations ADD CONSTRAINT fk_v22_observation_document_run FOREIGN KEY(document_id,run_id) REFERENCES v22_run_documents(id,run_id) ON DELETE RESTRICT;
 END IF;
END $$;
`)
	return err
}
