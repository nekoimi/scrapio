package migrate

import "xorm.io/xorm"

type v22RunCenter struct{}

func init()                          { registerMigrate(new(v22RunCenter)) }
func (*v22RunCenter) Version() int64 { return 2026_10_10_008 }
func (*v22RunCenter) Desc() string   { return "v2.2 运行中心固定版本重试来源" }
func (*v22RunCenter) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
ALTER TABLE v22_runs ADD COLUMN IF NOT EXISTS retry_of VARCHAR(36) REFERENCES v22_runs(id) ON DELETE RESTRICT;
ALTER TABLE v22_runs ADD COLUMN IF NOT EXISTS retry_scope VARCHAR(16) NOT NULL DEFAULT '' CHECK((retry_of IS NULL AND retry_scope='') OR (retry_of IS NOT NULL AND retry_scope='full_run'));
CREATE INDEX IF NOT EXISTS idx_v22_runs_retry_of ON v22_runs(owner_id,retry_of);
`)
	return err
}
