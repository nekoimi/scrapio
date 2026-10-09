package migrate

import "xorm.io/xorm"

type v22BrowserCapture struct{}

func init()                               { registerMigrate(new(v22BrowserCapture)) }
func (*v22BrowserCapture) Version() int64 { return 2026_10_09_003 }
func (*v22BrowserCapture) Desc() string   { return "新增 v2.2 浏览器快照来源与文档基址" }
func (*v22BrowserCapture) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`ALTER TABLE v22_captures ADD COLUMN IF NOT EXISTS session_id VARCHAR(36) NOT NULL DEFAULT '', ADD COLUMN IF NOT EXISTS page_state_id VARCHAR(256) NOT NULL DEFAULT '', ADD COLUMN IF NOT EXISTS base_url TEXT NOT NULL DEFAULT '';`)
	return err
}
