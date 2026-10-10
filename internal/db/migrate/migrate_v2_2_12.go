package migrate

import "xorm.io/xorm"

type v22DataReads struct{}

func init()                          { registerMigrate(new(v22DataReads)) }
func (*v22DataReads) Version() int64 { return 2026_10_10_004 }
func (*v22DataReads) Desc() string   { return "新增 v2.2 数据稳定分页与来源统计索引" }
func (*v22DataReads) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE INDEX IF NOT EXISTS idx_v22_records_first ON v22_table_records(table_id,first_observed_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS idx_v22_records_last ON v22_table_records(table_id,last_observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_v22_observations_table_collector ON v22_table_observations(table_id,collector_id);
`)
	return err
}
