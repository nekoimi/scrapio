package migrate

import "xorm.io/xorm"

type v22EditorCommands struct{}

func init()                               { registerMigrate(new(v22EditorCommands)) }
func (*v22EditorCommands) Version() int64 { return 2026_10_09_001 }
func (*v22EditorCommands) Desc() string   { return "新增 v2.2 编辑动作回执与步骤检查点" }
func (*v22EditorCommands) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_editor_commands (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 session_id VARCHAR(36) NOT NULL REFERENCES v22_browser_sessions(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 request JSONB NOT NULL,
 status VARCHAR(24) NOT NULL CHECK (status IN ('queued','running','succeeded','failed','uncertain')),
 result JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 deadline_at TIMESTAMPTZ NOT NULL,
 UNIQUE(owner_id, session_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_editor_commands_session ON v22_editor_commands(owner_id,session_id,created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_v22_editor_commands_active ON v22_editor_commands(session_id) WHERE status IN ('queued','running','uncertain');
CREATE TABLE IF NOT EXISTS v22_editor_checkpoints (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 name VARCHAR(160) NOT NULL,
 draft_revision INTEGER NOT NULL,
 through_step_id VARCHAR(128) NOT NULL,
 snapshot JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_v22_editor_checkpoints_owner ON v22_editor_checkpoints(owner_id,collector_id,created_at DESC);
`)
	return err
}
