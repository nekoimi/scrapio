package migrate

import "xorm.io/xorm"

type recordPluginSubscriptions struct{}

func init()                                       { registerMigrate(new(recordPluginSubscriptions)) }
func (*recordPluginSubscriptions) Version() int64 { return 2026_09_26_011 }
func (*recordPluginSubscriptions) Desc() string {
	return "通用记录插件订阅和事务投递队列"
}
func (*recordPluginSubscriptions) Exec(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	_, err := s.Exec(`CREATE TABLE IF NOT EXISTS plugin_subscriptions (
 id BIGSERIAL PRIMARY KEY, dataset_id BIGINT NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
 workflow_id BIGINT REFERENCES workflows(id) ON DELETE CASCADE,
 plugin_code VARCHAR(128) NOT NULL, event_type VARCHAR(64) NOT NULL CHECK(event_type IN ('record.created','record.updated')),
 enabled BOOLEAN NOT NULL DEFAULT FALSE, url_field VARCHAR(128) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
 );
 CREATE UNIQUE INDEX IF NOT EXISTS idx_plugin_subscriptions_scope ON plugin_subscriptions(dataset_id, COALESCE(workflow_id,0), plugin_code,event_type);
 ALTER TABLE plugin_tasks ALTER COLUMN resource_id DROP NOT NULL;
 ALTER TABLE plugin_tasks ADD COLUMN IF NOT EXISTS record_id BIGINT REFERENCES records(id) ON DELETE RESTRICT;
 ALTER TABLE plugin_tasks ADD COLUMN IF NOT EXISTS observation_id BIGINT REFERENCES record_observations(id) ON DELETE RESTRICT;
 ALTER TABLE plugin_tasks ADD COLUMN IF NOT EXISTS subscription_id BIGINT REFERENCES plugin_subscriptions(id) ON DELETE SET NULL;
 ALTER TABLE plugin_tasks ADD COLUMN IF NOT EXISTS dataset_id BIGINT REFERENCES datasets(id) ON DELETE RESTRICT;
 ALTER TABLE plugin_tasks ADD COLUMN IF NOT EXISTS workflow_id BIGINT REFERENCES workflows(id) ON DELETE RESTRICT;
 ALTER TABLE plugin_tasks DROP CONSTRAINT IF EXISTS plugin_tasks_subject;
 ALTER TABLE plugin_tasks ADD CONSTRAINT plugin_tasks_subject CHECK
 ((resource_id IS NOT NULL AND record_id IS NULL) OR (resource_id IS NULL AND record_id IS NOT NULL AND observation_id IS NOT NULL AND dataset_id IS NOT NULL));
 CREATE INDEX IF NOT EXISTS idx_plugin_tasks_dataset ON plugin_tasks(dataset_id,created_at DESC);
 CREATE INDEX IF NOT EXISTS idx_plugin_tasks_record ON plugin_tasks(record_id,created_at DESC);`)
	if err != nil {
		return err
	}
	return s.Commit()
}
