package migrate

import "xorm.io/xorm"

type aiAssistSuggestions struct{}

func init()                                 { registerMigrate(new(aiAssistSuggestions)) }
func (*aiAssistSuggestions) Version() int64 { return 2026_09_26_012 }
func (*aiAssistSuggestions) Desc() string   { return "AI 规则建议人工审核与请求限额" }
func (*aiAssistSuggestions) Exec(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	_, err := s.Exec(`CREATE TABLE IF NOT EXISTS ai_assist_requests (
 id BIGSERIAL PRIMARY KEY, workflow_id BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
 version_id BIGINT NOT NULL REFERENCES workflow_versions(id) ON DELETE CASCADE,
 sample_id BIGINT NOT NULL REFERENCES workflow_samples(id) ON DELETE CASCADE,
 status VARCHAR(32) NOT NULL CHECK (status IN ('reserved','succeeded','failed')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ
 );
 CREATE INDEX IF NOT EXISTS idx_ai_assist_requests_daily ON ai_assist_requests(created_at);
 CREATE TABLE IF NOT EXISTS ai_rule_suggestions (
 id BIGSERIAL PRIMARY KEY, workflow_id BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
 version_id BIGINT NOT NULL REFERENCES workflow_versions(id) ON DELETE RESTRICT,
 sample_id BIGINT NOT NULL REFERENCES workflow_samples(id) ON DELETE RESTRICT,
 request_id BIGINT NOT NULL UNIQUE REFERENCES ai_assist_requests(id) ON DELETE RESTRICT,
 model VARCHAR(128) NOT NULL, content_hash VARCHAR(128) NOT NULL,
 result JSONB NOT NULL, status VARCHAR(32) NOT NULL DEFAULT 'pending_review'
 CHECK (status IN ('pending_review','accepted','rejected')),
 input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
 reviewed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CONSTRAINT ai_rule_suggestions_result_size CHECK (pg_column_size(result)<=65536)
 );
 CREATE INDEX IF NOT EXISTS idx_ai_rule_suggestions_version ON ai_rule_suggestions(version_id,created_at DESC);`)
	if err != nil {
		return err
	}
	return s.Commit()
}
