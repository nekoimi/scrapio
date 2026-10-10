package migrate

import "xorm.io/xorm"

type v22RepairDrafts struct{}

func init()                             { registerMigrate(new(v22RepairDrafts)) }
func (*v22RepairDrafts) Version() int64 { return 2026_10_10_009 }
func (*v22RepairDrafts) Desc() string   { return "v2.2 失败输入与修复草稿持久关联" }
func (*v22RepairDrafts) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`CREATE TABLE IF NOT EXISTS v22_repair_drafts (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 source_collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 target_collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 run_id VARCHAR(36) NOT NULL REFERENCES v22_runs(id) ON DELETE RESTRICT,
 capture_id VARCHAR(36) REFERENCES v22_captures(id) ON DELETE SET NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 context JSONB NOT NULL CHECK(jsonb_typeof(context)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_v22_repairs_target ON v22_repair_drafts(owner_id,target_collector_id,created_at DESC);
`)
	return err
}
