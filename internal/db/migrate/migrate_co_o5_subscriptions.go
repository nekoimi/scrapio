package migrate

import "xorm.io/xorm"

type coSubscriptions struct{}

func init()                             { registerMigrate(new(coSubscriptions)) }
func (*coSubscriptions) Version() int64 { return 2026_10_14_003 }
func (*coSubscriptions) Desc() string {
	return "transactional table change streams and durable task subscriptions"
}
func (*coSubscriptions) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 CREATE TABLE IF NOT EXISTS co_table_streams(table_id VARCHAR(36) PRIMARY KEY REFERENCES co_data_tables(id),sequence BIGINT NOT NULL DEFAULT 0,retained_after BIGINT NOT NULL DEFAULT 0);
 INSERT INTO co_table_streams(table_id) SELECT id FROM co_data_tables ON CONFLICT DO NOTHING;
 CREATE TABLE IF NOT EXISTS co_record_changes(table_id VARCHAR(36) NOT NULL REFERENCES co_data_tables(id),sequence BIGINT NOT NULL,owner_id BIGINT NOT NULL REFERENCES admin(id),record_id VARCHAR(36) NOT NULL REFERENCES co_records(id),revision INT NOT NULL,schema_version INT NOT NULL,kind TEXT NOT NULL CHECK(kind IN ('created','updated')),values_json JSONB NOT NULL,observation_id VARCHAR(36) NOT NULL REFERENCES co_observations(id),accounted_bytes BIGINT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(table_id,sequence),UNIQUE(record_id,revision));
 CREATE INDEX IF NOT EXISTS idx_co_changes_retention ON co_record_changes(created_at,table_id,sequence);
 CREATE TABLE IF NOT EXISTS co_subscriptions(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),task_id VARCHAR(36) NOT NULL REFERENCES co_tasks(id),task_version_id VARCHAR(36) NOT NULL REFERENCES co_task_versions(id),table_id VARCHAR(36) NOT NULL REFERENCES co_data_tables(id),schema_version INT NOT NULL,schema_hash TEXT NOT NULL,mapping JSONB NOT NULL,write_tables JSONB NOT NULL,initial_mode TEXT NOT NULL CHECK(initial_mode IN ('backfill','future')),batch_size INT NOT NULL CHECK(batch_size BETWEEN 1 AND 100),enabled BOOLEAN NOT NULL,revision INT NOT NULL DEFAULT 1,cursor BIGINT NOT NULL,backfill JSONB NOT NULL DEFAULT '[]',backfill_cursor INT NOT NULL DEFAULT 0,accounted_bytes BIGINT NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'ready',error JSONB NOT NULL DEFAULT '{}',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE UNIQUE INDEX IF NOT EXISTS idx_co_subscription_task ON co_subscriptions(task_id) WHERE status<>'retired';
 ALTER TABLE co_subscriptions ADD COLUMN IF NOT EXISTS last_checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
 ALTER TABLE co_table_streams ADD COLUMN IF NOT EXISTS last_cleanup_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
 CREATE INDEX IF NOT EXISTS idx_co_subscription_due ON co_subscriptions(created_at,id) WHERE enabled AND status='ready';
 CREATE INDEX IF NOT EXISTS idx_co_subscription_table ON co_subscriptions(table_id,cursor);
 CREATE TABLE IF NOT EXISTS co_subscription_batches(id VARCHAR(36) PRIMARY KEY,subscription_id VARCHAR(36) NOT NULL REFERENCES co_subscriptions(id),batch_key TEXT NOT NULL,kind TEXT NOT NULL,first_sequence BIGINT NOT NULL,last_sequence BIGINT NOT NULL,input_count INT NOT NULL,run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(subscription_id,batch_key));
 ALTER TABLE co_storage_usage ADD COLUMN IF NOT EXISTS change_bytes BIGINT NOT NULL DEFAULT 0 CHECK(change_bytes>=0);
 `)
	return err
}
