package migrate

import "xorm.io/xorm"

type v22Credentials struct{}

func init()                            { registerMigrate(new(v22Credentials)) }
func (*v22Credentials) Version() int64 { return 2026_10_10_012 }
func (*v22Credentials) Desc() string {
	return "v2.2 scoped encrypted credentials and authorized sessions"
}
func (*v22Credentials) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`CREATE TABLE IF NOT EXISTS v22_credentials (
 id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 revision INT NOT NULL,name VARCHAR(80) NOT NULL,kind VARCHAR(32) NOT NULL,origin TEXT NOT NULL,storage VARCHAR(20) NOT NULL,
 header TEXT NOT NULL DEFAULT '',prefix TEXT NOT NULL DEFAULT '',env TEXT NOT NULL DEFAULT '',ciphertext TEXT NOT NULL DEFAULT '',key_id TEXT NOT NULL DEFAULT '',
 mask_selectors JSONB NOT NULL DEFAULT '[]',fingerprint TEXT NOT NULL,create_fingerprint TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,revoked_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE INDEX IF NOT EXISTS idx_v22_credentials_owner ON v22_credentials(owner_id,created_at DESC,id);
 ALTER TABLE v22_browser_sessions ADD COLUMN IF NOT EXISTS credential_ref TEXT NOT NULL DEFAULT '';
 ALTER TABLE v22_browser_sessions ADD COLUMN IF NOT EXISTS credential_revision INT NOT NULL DEFAULT 0;
 `)
	return err
}
