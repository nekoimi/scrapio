package migrate

import "xorm.io/xorm"

type v22BrowserSessions struct{}

func init()                                { registerMigrate(new(v22BrowserSessions)) }
func (*v22BrowserSessions) Version() int64 { return 2026_09_30_002 }
func (*v22BrowserSessions) Desc() string   { return "新增 v2.2 浏览器编辑会话授权与状态" }

func (*v22BrowserSessions) Exec(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	if _, err := s.Exec(`
CREATE TABLE IF NOT EXISTS v22_browser_sessions (
 id VARCHAR(36) PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 draft_revision INTEGER NOT NULL CHECK (draft_revision > 0),
 target_url TEXT NOT NULL,
 status VARCHAR(24) NOT NULL CHECK (status IN ('creating','ready','disconnected','expired','closed','failed')),
 expires_at TIMESTAMPTZ NOT NULL,
 page_state_id VARCHAR(256) NOT NULL DEFAULT '',
 current_url TEXT NOT NULL DEFAULT '',
 viewport_width INTEGER NOT NULL,
 viewport_height INTEGER NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 closed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_v22_browser_sessions_owner_expiry ON v22_browser_sessions(owner_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_v22_browser_sessions_expiry ON v22_browser_sessions(expires_at) WHERE status IN ('creating','ready','disconnected');
CREATE TABLE IF NOT EXISTS v22_browser_session_keys (
 id BIGSERIAL PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE CASCADE,
 key VARCHAR(128) NOT NULL,
 session_id VARCHAR(36) NOT NULL REFERENCES v22_browser_sessions(id) ON DELETE CASCADE,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE CASCADE,
 draft_revision INTEGER NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(owner_id, key)
);
`); err != nil {
		return err
	}
	return s.Commit()
}
