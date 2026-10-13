package migrate

import "xorm.io/xorm"

type coDaily struct{}

func init()                     { registerMigrate(new(coDaily)) }
func (*coDaily) Version() int64 { return 2026_10_13_004 }
func (*coDaily) Desc() string   { return "frozen sources, schedules and independent task governance" }
func (*coDaily) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 ALTER TABLE co_tasks ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT FALSE;
 ALTER TABLE co_runs ADD COLUMN IF NOT EXISTS last_claim_at TIMESTAMPTZ;
 ALTER TABLE co_runs ADD COLUMN IF NOT EXISTS last_maintenance_at TIMESTAMPTZ;
 ALTER TABLE co_runs ADD COLUMN IF NOT EXISTS last_dispatch_at TIMESTAMPTZ;
 CREATE TABLE IF NOT EXISTS co_source_snapshots(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),kind TEXT NOT NULL,collector_version_id VARCHAR(36) NOT NULL REFERENCES co_collector_versions(id),content_hash TEXT NOT NULL,row_count INT NOT NULL,valid_count INT NOT NULL,rows_json JSONB NOT NULL,provenance JSONB NOT NULL,original_content TEXT NOT NULL DEFAULT '',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),expired BOOLEAN NOT NULL DEFAULT FALSE,CHECK(row_count BETWEEN 1 AND 1000));
 CREATE TABLE IF NOT EXISTS co_schedules(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),task_id VARCHAR(36) NOT NULL REFERENCES co_tasks(id),task_version_id VARCHAR(36) NOT NULL REFERENCES co_task_versions(id),interval_seconds INT NOT NULL CHECK(interval_seconds BETWEEN 60 AND 2592000),enabled BOOLEAN NOT NULL DEFAULT TRUE,next_run_at TIMESTAMPTZ NOT NULL,revision INT NOT NULL DEFAULT 1,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(task_id));
 CREATE TABLE IF NOT EXISTS co_triggers(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),task_id VARCHAR(36) NOT NULL REFERENCES co_tasks(id),task_version_id VARCHAR(36) NOT NULL REFERENCES co_task_versions(id),schedule_id VARCHAR(36) REFERENCES co_schedules(id),trigger_key TEXT NOT NULL,kind TEXT NOT NULL,status TEXT NOT NULL,run_id VARCHAR(36) REFERENCES co_runs(id),data JSONB NOT NULL DEFAULT '{}',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(owner_id,trigger_key));
 CREATE TABLE IF NOT EXISTS co_success_history(owner_id BIGINT NOT NULL REFERENCES admin(id),collector_version_id VARCHAR(36) NOT NULL REFERENCES co_collector_versions(id),input_hash TEXT NOT NULL,policy_hash TEXT NOT NULL,item_id VARCHAR(36) NOT NULL REFERENCES co_work_items(id),run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),succeeded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(owner_id,collector_version_id,input_hash,policy_hash));
 CREATE TABLE IF NOT EXISTS co_site_limits(site_key TEXT PRIMARY KEY,next_start_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE TABLE IF NOT EXISTS co_storage_usage(owner_id BIGINT PRIMARY KEY REFERENCES admin(id),source_bytes BIGINT NOT NULL DEFAULT 0,output_bytes BIGINT NOT NULL DEFAULT 0,CHECK(source_bytes>=0),CHECK(output_bytes>=0));
 INSERT INTO co_storage_usage(owner_id,output_bytes) SELECT r.owner_id,COALESCE(SUM(octet_length(o.values_json::text)+octet_length(o.source::text)),0) FROM co_runs r JOIN co_run_outputs o ON o.run_id=r.id GROUP BY r.owner_id ON CONFLICT DO NOTHING;
 ALTER TABLE co_source_snapshots ADD COLUMN IF NOT EXISTS accounted_bytes BIGINT NOT NULL DEFAULT 0;
 CREATE INDEX IF NOT EXISTS idx_co_schedule_due ON co_schedules(next_run_at) WHERE enabled;
 CREATE INDEX IF NOT EXISTS idx_co_triggers_owner ON co_triggers(owner_id,created_at DESC);
 CREATE INDEX IF NOT EXISTS idx_co_source_owner ON co_source_snapshots(owner_id,created_at DESC);
 `)
	return err
}
