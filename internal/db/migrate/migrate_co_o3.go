package migrate

import "xorm.io/xorm"

type coGraph struct{}

func init()                     { registerMigrate(new(coGraph)) }
func (*coGraph) Version() int64 { return 2026_10_13_003 }
func (*coGraph) Desc() string   { return "durable stage graph, transactional dispatch plans and lineage" }
func (*coGraph) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 ALTER TABLE co_stage_runs ADD COLUMN IF NOT EXISTS blocked_reason TEXT NOT NULL DEFAULT '';
 ALTER TABLE co_stage_runs ADD COLUMN IF NOT EXISTS last_claim_at TIMESTAMPTZ;
 ALTER TABLE co_work_items ADD COLUMN IF NOT EXISTS site_key TEXT NOT NULL DEFAULT '';
 CREATE UNIQUE INDEX IF NOT EXISTS idx_co_outputs_same_run ON co_run_outputs(run_id,namespace,id);
 CREATE UNIQUE INDEX IF NOT EXISTS idx_co_runs_namespace ON co_runs(id,namespace);
 CREATE TABLE IF NOT EXISTS co_dispatch_plans(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,namespace TEXT NOT NULL,output_id VARCHAR(36) NOT NULL,edge_id TEXT NOT NULL,from_stage TEXT NOT NULL,to_stage TEXT NOT NULL,mapping_hash TEXT NOT NULL,rows_json JSONB NOT NULL,next_ordinal INT NOT NULL DEFAULT 0,total INT NOT NULL,status TEXT NOT NULL DEFAULT 'pending',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,namespace,output_id) REFERENCES co_run_outputs(run_id,namespace,id),UNIQUE(output_id,edge_id),UNIQUE(run_id,id),CHECK(total BETWEEN 0 AND 100),CHECK(next_ordinal BETWEEN 0 AND total));
 CREATE TABLE IF NOT EXISTS co_dispatches(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,plan_id VARCHAR(36) NOT NULL,output_id VARCHAR(36) NOT NULL,edge_id TEXT NOT NULL,expansion_ordinal INT NOT NULL,element_key TEXT NOT NULL,status TEXT NOT NULL,input_snapshot JSONB,error JSONB NOT NULL DEFAULT '{}',child_item_id VARCHAR(36),created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,plan_id) REFERENCES co_dispatch_plans(run_id,id),FOREIGN KEY(run_id,child_item_id) REFERENCES co_work_items(run_id,id),UNIQUE(output_id,edge_id,expansion_ordinal));
 CREATE TABLE IF NOT EXISTS co_lineage(target_item_id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,source_item_id VARCHAR(36) NOT NULL,source_output_id VARCHAR(36) NOT NULL,namespace TEXT NOT NULL,edge_id TEXT NOT NULL,mapping_hash TEXT NOT NULL,element_key TEXT NOT NULL,FOREIGN KEY(run_id,target_item_id) REFERENCES co_work_items(run_id,id),FOREIGN KEY(run_id,source_item_id) REFERENCES co_work_items(run_id,id),FOREIGN KEY(run_id,namespace,source_output_id) REFERENCES co_run_outputs(run_id,namespace,id));
 CREATE INDEX IF NOT EXISTS idx_co_plans_pending ON co_dispatch_plans(run_id,status,created_at,id);
 CREATE INDEX IF NOT EXISTS idx_co_dispatches_run ON co_dispatches(run_id,created_at DESC,id);
 CREATE INDEX IF NOT EXISTS idx_co_lineage_source ON co_lineage(source_output_id);
 `)
	return err
}
