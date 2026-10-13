package migrate

import "xorm.io/xorm"

type coInputMerge struct{}

func init()                          { registerMigrate(new(coInputMerge)) }
func (*coInputMerge) Version() int64 { return 2026_10_13_005 }
func (*coInputMerge) Desc() string   { return "opt-in stage input merging with complete source receipts" }
func (*coInputMerge) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 ALTER TABLE co_work_items ADD COLUMN IF NOT EXISTS dedup_key TEXT;
 CREATE UNIQUE INDEX IF NOT EXISTS idx_co_items_dedup ON co_work_items(run_id,stage_run_id,dedup_key) WHERE dedup_key IS NOT NULL;
 CREATE TABLE IF NOT EXISTS co_item_sources(run_id VARCHAR(36) NOT NULL,item_id VARCHAR(36) NOT NULL,input_id VARCHAR(36) PRIMARY KEY,merged BOOLEAN NOT NULL DEFAULT FALSE,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,item_id) REFERENCES co_work_items(run_id,id),FOREIGN KEY(run_id,input_id) REFERENCES co_run_inputs(run_id,id));
 INSERT INTO co_item_sources(run_id,item_id,input_id) SELECT run_id,id,input_id FROM co_work_items ON CONFLICT(input_id) DO NOTHING;
 CREATE INDEX IF NOT EXISTS idx_co_item_sources_item ON co_item_sources(run_id,item_id,input_id);
 ALTER TABLE co_lineage DROP CONSTRAINT IF EXISTS co_lineage_pkey;
 ALTER TABLE co_lineage ADD PRIMARY KEY(target_item_id,source_output_id,edge_id,element_key);
 `)
	return err
}
