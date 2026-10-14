package migrate

import "xorm.io/xorm"

type coGather struct{}

func init()                      { registerMigrate(new(coGather)) }
func (*coGather) Version() int64 { return 2026_10_14_002 }
func (*coGather) Desc() string {
	return "finite keyed join and batch barrier with durable gather receipts"
}
func (*coGather) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 ALTER TABLE co_stage_runs ADD COLUMN IF NOT EXISTS gather_sealed BOOLEAN NOT NULL DEFAULT FALSE;
 CREATE TABLE IF NOT EXISTS co_gather_groups(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,stage_run_id VARCHAR(36) NOT NULL,group_key TEXT NOT NULL,values_json JSONB NOT NULL,members JSONB NOT NULL,status TEXT NOT NULL,error JSONB NOT NULL DEFAULT '{}',child_item_id VARCHAR(36),created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,stage_run_id) REFERENCES co_stage_runs(run_id,id),FOREIGN KEY(run_id,child_item_id) REFERENCES co_work_items(run_id,id),UNIQUE(stage_run_id,group_key));
 CREATE INDEX IF NOT EXISTS idx_co_gather_pending ON co_gather_groups(run_id,stage_run_id,status);
 CREATE INDEX IF NOT EXISTS idx_co_gather_page ON co_gather_groups(run_id,created_at DESC,id);
 CREATE INDEX IF NOT EXISTS idx_co_dispatches_gather ON co_dispatches(run_id,plan_id,status);
 `)
	return err
}
