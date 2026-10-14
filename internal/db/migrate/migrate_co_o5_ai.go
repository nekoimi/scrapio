package migrate

import "xorm.io/xorm"

type coAIAssist struct{}

func init()                        { registerMigrate(new(coAIAssist)) }
func (*coAIAssist) Version() int64 { return 2026_10_14_005 }
func (*coAIAssist) Desc() string {
	return "bounded independent AI previews, one-shot calls and explicit draft adoption"
}
func (*coAIAssist) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 CREATE TABLE IF NOT EXISTS co_ai_suggestions(
 id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),
 resource_kind TEXT NOT NULL CHECK(resource_kind IN ('collector','task','run')),resource_id VARCHAR(36) NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('selectors','mapping','failure')),base_revision INT NOT NULL,base_hash TEXT NOT NULL,
 source JSONB NOT NULL,prompt JSONB NOT NULL,prompt_version TEXT NOT NULL DEFAULT 'co.ai.assist.v1',model TEXT NOT NULL,gateway_hash TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('prepared','calling','ready','accepted','failed','unknown','rejected','applied')),
 result JSONB NOT NULL DEFAULT '{}',validated_hash TEXT NOT NULL DEFAULT '',
 call_operation_id VARCHAR(36) REFERENCES co_operations(id) DEFERRABLE INITIALLY DEFERRED,called_at TIMESTAMPTZ,
 reserved_bytes BIGINT NOT NULL DEFAULT 0,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
 );
 CREATE INDEX IF NOT EXISTS idx_co_ai_resource ON co_ai_suggestions(owner_id,resource_kind,resource_id,created_at DESC,id DESC);
 CREATE INDEX IF NOT EXISTS idx_co_ai_daily ON co_ai_suggestions(owner_id,called_at) WHERE called_at IS NOT NULL;
 `)
	return err
}
