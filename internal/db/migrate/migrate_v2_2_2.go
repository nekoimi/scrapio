package migrate

import "xorm.io/xorm"

type v22CollectorValidation struct{}

func init()                                    { registerMigrate(new(v22CollectorValidation)) }
func (*v22CollectorValidation) Version() int64 { return 2026_09_30_001 }
func (*v22CollectorValidation) Desc() string {
	return "为 v2.2 草稿增加 revision 绑定的结构检查状态"
}

func (*v22CollectorValidation) Exec(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	if _, err := s.Exec(`
ALTER TABLE v22_collectors
  ADD COLUMN IF NOT EXISTS validated_revision INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS validation_summary JSONB NOT NULL DEFAULT '{"valid":false,"errors":[]}'::jsonb;
`); err != nil {
		return err
	}
	return s.Commit()
}
