package migrate

import "xorm.io/xorm"

type coTasks struct{}

func init()                     { registerMigrate(new(coTasks)) }
func (*coTasks) Version() int64 { return 2026_10_13_002 }
func (*coTasks) Desc() string {
	return "independent single-stage durable tasks, isolated data and atomic page checkpoints"
}
func (*coTasks) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 CREATE TABLE IF NOT EXISTS co_data_tables(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),name VARCHAR(64) NOT NULL,current_schema_version INT NOT NULL DEFAULT 1,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(owner_id,id));
 CREATE TABLE IF NOT EXISTS co_schema_versions(table_id VARCHAR(36) NOT NULL REFERENCES co_data_tables(id),version INT NOT NULL,schema JSONB NOT NULL,schema_hash TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(table_id,version));
 CREATE TABLE IF NOT EXISTS co_tasks(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),name VARCHAR(64) NOT NULL,revision INT NOT NULL DEFAULT 0,definition JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(owner_id,id));
 CREATE TABLE IF NOT EXISTS co_task_versions(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL,task_id VARCHAR(36) NOT NULL,number INT NOT NULL,revision INT NOT NULL,collector_version_id VARCHAR(36) NOT NULL REFERENCES co_collector_versions(id),manifest JSONB NOT NULL,manifest_hash TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(owner_id,task_id) REFERENCES co_tasks(owner_id,id),UNIQUE(task_id,number),UNIQUE(task_id,revision),UNIQUE(owner_id,id));
 CREATE TABLE IF NOT EXISTS co_runs(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL,task_version_id VARCHAR(36) NOT NULL,manifest JSONB NOT NULL,namespace TEXT NOT NULL CHECK(namespace IN ('formal','trial')),status TEXT NOT NULL,control_epoch BIGINT NOT NULL DEFAULT 0,preparing_cursor INT NOT NULL DEFAULT 0,input_count INT NOT NULL,output_count INT NOT NULL DEFAULT 0,event_seq BIGINT NOT NULL DEFAULT 0,retry_of VARCHAR(36),retry_scope JSONB NOT NULL DEFAULT '[]',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),finished_at TIMESTAMPTZ,FOREIGN KEY(owner_id,task_version_id) REFERENCES co_task_versions(owner_id,id),FOREIGN KEY(owner_id,retry_of) REFERENCES co_runs(owner_id,id),UNIQUE(owner_id,id));
 CREATE INDEX IF NOT EXISTS idx_co_runs_owner ON co_runs(owner_id,created_at DESC,id);
 CREATE TABLE IF NOT EXISTS co_stage_runs(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),stage_id TEXT NOT NULL,collector_version_id VARCHAR(36) NOT NULL REFERENCES co_collector_versions(id),status TEXT NOT NULL DEFAULT 'preparing',counters JSONB NOT NULL DEFAULT '{}',input_closed BOOLEAN NOT NULL DEFAULT FALSE,close_reason TEXT NOT NULL DEFAULT '',UNIQUE(run_id,stage_id),UNIQUE(run_id,id));
 CREATE TABLE IF NOT EXISTS co_run_inputs(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),ordinal INT NOT NULL,source_key TEXT NOT NULL,values_json JSONB NOT NULL,source JSONB NOT NULL,materialized_at TIMESTAMPTZ,UNIQUE(run_id,ordinal),UNIQUE(run_id,id));
 CREATE TABLE IF NOT EXISTS co_work_items(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),stage_run_id VARCHAR(36) NOT NULL,input_id VARCHAR(36) NOT NULL,source_key TEXT NOT NULL,input_values JSONB NOT NULL,input_hash TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'queued',attempt INT NOT NULL DEFAULT 0,next_retry_at TIMESTAMPTZ,lease_token VARCHAR(36),lease_until TIMESTAMPTZ,control_epoch BIGINT NOT NULL DEFAULT 0,checkpoint JSONB NOT NULL DEFAULT '{}',error JSONB NOT NULL DEFAULT '{}',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,stage_run_id) REFERENCES co_stage_runs(run_id,id),FOREIGN KEY(run_id,input_id) REFERENCES co_run_inputs(run_id,id),UNIQUE(run_id,stage_run_id,source_key),UNIQUE(run_id,id));
 CREATE INDEX IF NOT EXISTS idx_co_items_claim ON co_work_items(status,next_retry_at,created_at);
 CREATE TABLE IF NOT EXISTS co_work_attempts(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,item_id VARCHAR(36) NOT NULL,number INT NOT NULL,lease_token VARCHAR(36) NOT NULL,session_id VARCHAR(36) NOT NULL,status TEXT NOT NULL,command_refs JSONB NOT NULL DEFAULT '[]',error JSONB NOT NULL DEFAULT '{}',started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),finished_at TIMESTAMPTZ,FOREIGN KEY(run_id,item_id) REFERENCES co_work_items(run_id,id),UNIQUE(item_id,number));
 CREATE TABLE IF NOT EXISTS co_records(id VARCHAR(36) PRIMARY KEY,table_id VARCHAR(36) NOT NULL REFERENCES co_data_tables(id),canonical_key TEXT NOT NULL,values_json JSONB NOT NULL,revision INT NOT NULL,schema_version INT NOT NULL,last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(table_id,canonical_key));
 CREATE TABLE IF NOT EXISTS co_observations(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,item_id VARCHAR(36) NOT NULL,record_id VARCHAR(36) REFERENCES co_records(id),output_identity TEXT NOT NULL,values_json JSONB NOT NULL,schema_hash TEXT NOT NULL,decision TEXT NOT NULL,diagnostic JSONB NOT NULL DEFAULT '{}',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,item_id) REFERENCES co_work_items(run_id,id));
 CREATE TABLE IF NOT EXISTS co_record_revisions(record_id VARCHAR(36) NOT NULL REFERENCES co_records(id),revision INT NOT NULL,observation_id VARCHAR(36) NOT NULL REFERENCES co_observations(id),values_json JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(record_id,revision));
 CREATE TABLE IF NOT EXISTS co_run_outputs(id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,item_id VARCHAR(36) NOT NULL,namespace TEXT NOT NULL,output_identity TEXT NOT NULL,values_json JSONB NOT NULL,schema_hash TEXT NOT NULL,content_hash TEXT NOT NULL,observation_id VARCHAR(36) NOT NULL REFERENCES co_observations(id),decision TEXT NOT NULL,source JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),FOREIGN KEY(run_id,item_id) REFERENCES co_work_items(run_id,id),UNIQUE(item_id,output_identity));
 CREATE TABLE IF NOT EXISTS co_run_events(run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),sequence BIGINT NOT NULL,type TEXT NOT NULL,item_id VARCHAR(36),data JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(run_id,sequence));
 CREATE INDEX IF NOT EXISTS idx_co_tasks_owner ON co_tasks(owner_id,created_at DESC,id);
 CREATE INDEX IF NOT EXISTS idx_co_items_run ON co_work_items(run_id,status,created_at,id);
 CREATE INDEX IF NOT EXISTS idx_co_outputs_run ON co_run_outputs(run_id,created_at DESC,id);
 CREATE INDEX IF NOT EXISTS idx_co_observations_run ON co_observations(run_id,created_at DESC,id);
 CREATE INDEX IF NOT EXISTS idx_co_observations_item ON co_observations(item_id,decision);
 CREATE INDEX IF NOT EXISTS idx_co_observations_record ON co_observations(record_id,created_at DESC,id);
 CREATE INDEX IF NOT EXISTS idx_co_records_table ON co_records(table_id,id);
 `)
	return err
}
