package migrate

import "xorm.io/xorm"

type projectHealthIndexes struct{}

func init()                                  { registerMigrate(new(projectHealthIndexes)) }
func (*projectHealthIndexes) Version() int64 { return 2026_09_26_007 }
func (*projectHealthIndexes) Desc() string   { return "项目健康运行查询索引" }
func (*projectHealthIndexes) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow_recent ON workflow_runs(workflow_id,id DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_success ON workflow_runs(workflow_id,finished_at DESC) WHERE status='succeeded';
`)
	return err
}
