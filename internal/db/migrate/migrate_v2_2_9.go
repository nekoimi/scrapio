package migrate

import "xorm.io/xorm"

type v22Trials struct{}

func init()                       { registerMigrate(new(v22Trials)) }
func (*v22Trials) Version() int64 { return 2026_10_10_001 }
func (*v22Trials) Desc() string   { return "新增 v2.2 冻结试采、文档结果与有序事件" }
func (*v22Trials) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_trials (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 collector_revision INTEGER NOT NULL,
 definition JSONB NOT NULL, definition_hash VARCHAR(64) NOT NULL,
 entry_type VARCHAR(16) NOT NULL, input JSONB NOT NULL, fixed_inputs JSONB NOT NULL,
 output_schema JSONB NOT NULL,
 status VARCHAR(16) NOT NULL CHECK(status IN ('queued','running','succeeded','partial','limited','failed','cancelled')),
 cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
 session_id VARCHAR(36) NOT NULL DEFAULT '', current_step VARCHAR(128) NOT NULL DEFAULT '',
 current_stage VARCHAR(32) NOT NULL DEFAULT '', event_seq BIGINT NOT NULL DEFAULT 1,
 summary JSONB NOT NULL DEFAULT '{}',
 idempotency_key VARCHAR(128) NOT NULL, fingerprint VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_trials_owner_collector ON v22_trials(owner_id,collector_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS idx_v22_trials_queue ON v22_trials(created_at,id) WHERE status='queued';
CREATE TABLE IF NOT EXISTS v22_trial_documents (
 id VARCHAR(36) PRIMARY KEY, trial_id VARCHAR(36) NOT NULL REFERENCES v22_trials(id) ON DELETE CASCADE,
 sequence BIGINT NOT NULL, content TEXT NOT NULL, result JSONB NOT NULL,
 UNIQUE(trial_id,sequence)
);
CREATE TABLE IF NOT EXISTS v22_trial_events (
 trial_id VARCHAR(36) NOT NULL REFERENCES v22_trials(id) ON DELETE CASCADE,
 sequence BIGINT NOT NULL, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(trial_id,sequence)
);`)
	return err
}
