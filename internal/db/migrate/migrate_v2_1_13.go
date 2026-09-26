package migrate

import "xorm.io/xorm"

type collectorMaintenance struct{}

func init()                                  { registerMigrate(new(collectorMaintenance)) }
func (*collectorMaintenance) Version() int64 { return 2026_09_26_010 }
func (*collectorMaintenance) Desc() string   { return "采集器负责人和版本变更摘要" }
func (*collectorMaintenance) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`ALTER TABLE workflows ADD COLUMN IF NOT EXISTS owner_name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE workflow_versions ADD COLUMN IF NOT EXISTS change_summary VARCHAR(500) NOT NULL DEFAULT '';`)
	return err
}
