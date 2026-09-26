package migrate

import "xorm.io/xorm"

type dataConsumption struct{}

func init()                             { registerMigrate(new(dataConsumption)) }
func (*dataConsumption) Version() int64 { return 2026_09_26_006 }
func (*dataConsumption) Desc() string {
	return "数据集筛选视图、受控导出任务与查询索引"
}
func (*dataConsumption) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS record_views (
 id BIGSERIAL PRIMARY KEY, dataset_id BIGINT NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
 name VARCHAR(80) NOT NULL, filter JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(dataset_id,name), CONSTRAINT record_views_filter_object CHECK (jsonb_typeof(filter)='object')
);
CREATE TABLE IF NOT EXISTS record_export_jobs (
 id BIGSERIAL PRIMARY KEY, dataset_id BIGINT NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
 filter JSONB NOT NULL, status TEXT NOT NULL CHECK(status IN ('queued','running','ready','failed')),
 row_count INTEGER NOT NULL DEFAULT 0, content BYTEA, error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ,
 CONSTRAINT record_export_jobs_filter_object CHECK (jsonb_typeof(filter)='object')
);
CREATE INDEX IF NOT EXISTS idx_export_jobs_queued ON record_export_jobs(id) WHERE status='queued';
CREATE INDEX IF NOT EXISTS idx_observations_dataset_source_record ON record_observations(dataset_id,source_id,record_id);
CREATE INDEX IF NOT EXISTS idx_revisions_created ON record_revisions(created_at DESC);
`)
	return err
}
