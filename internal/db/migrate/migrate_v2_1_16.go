package migrate

import "xorm.io/xorm"

// Suggestions belong to a draft sample. Removing that sample must also remove
// its suggestions and request reservations, including in already migrated DBs.
type aiAssistSampleCleanup struct{}

func init() { registerMigrate(new(aiAssistSampleCleanup)) }

func (*aiAssistSampleCleanup) Version() int64 { return 2026_09_26_013 }
func (*aiAssistSampleCleanup) Desc() string   { return "允许删除已有 AI 建议的草稿样例" }

func (*aiAssistSampleCleanup) Exec(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	_, err := s.Exec(`
ALTER TABLE ai_rule_suggestions DROP CONSTRAINT IF EXISTS ai_rule_suggestions_sample_id_fkey;
ALTER TABLE ai_rule_suggestions ADD CONSTRAINT ai_rule_suggestions_sample_id_fkey
  FOREIGN KEY (sample_id) REFERENCES workflow_samples(id) ON DELETE CASCADE;
ALTER TABLE ai_rule_suggestions DROP CONSTRAINT IF EXISTS ai_rule_suggestions_request_id_fkey;
ALTER TABLE ai_rule_suggestions ADD CONSTRAINT ai_rule_suggestions_request_id_fkey
  FOREIGN KEY (request_id) REFERENCES ai_assist_requests(id) ON DELETE CASCADE;
ALTER TABLE ai_assist_requests DROP CONSTRAINT IF EXISTS ai_assist_requests_sample_id_fkey;
ALTER TABLE ai_assist_requests ADD CONSTRAINT ai_assist_requests_sample_id_fkey
  FOREIGN KEY (sample_id) REFERENCES workflow_samples(id) ON DELETE CASCADE;`)
	if err != nil {
		return err
	}
	return s.Commit()
}
