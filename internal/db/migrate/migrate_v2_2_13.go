package migrate

import "xorm.io/xorm"

type v22RunCheckpoints struct{}

func init()                               { registerMigrate(new(v22RunCheckpoints)) }
func (*v22RunCheckpoints) Version() int64 { return 2026_10_10_005 }
func (*v22RunCheckpoints) Desc() string   { return "新增 v2.2 连续运行持久检查点" }
func (*v22RunCheckpoints) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_run_checkpoints (
 id VARCHAR(36) PRIMARY KEY,
 run_id VARCHAR(36) NOT NULL REFERENCES v22_runs(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL,sequence BIGINT NOT NULL,
 document_id VARCHAR(36) NOT NULL,
 state VARCHAR(32) NOT NULL CHECK(state IN ('page_captured','page_complete','action_pending','awaiting_page','stopped')),
 payload JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(run_id,sequence),
 FOREIGN KEY(document_id,run_id) REFERENCES v22_run_documents(id,run_id) ON DELETE RESTRICT,
 FOREIGN KEY(run_id,attempt) REFERENCES v22_run_attempts(run_id,attempt) ON DELETE CASCADE
);
`)
	return err
}
