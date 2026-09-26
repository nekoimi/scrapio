package migrate

import "xorm.io/xorm"

type sampleExpectations struct{}

func init()                                { registerMigrate(new(sampleExpectations)) }
func (*sampleExpectations) Version() int64 { return 2026_09_26_008 }
func (*sampleExpectations) Desc() string   { return "样例预期与历史文档保留保护" }
func (*sampleExpectations) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
ALTER TABLE workflow_samples ADD COLUMN IF NOT EXISTS expected_outcome VARCHAR(16) NOT NULL DEFAULT 'success';
ALTER TABLE workflow_samples ADD COLUMN IF NOT EXISTS expected_error TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_samples DROP CONSTRAINT IF EXISTS workflow_samples_expected_outcome_check;
ALTER TABLE workflow_samples ADD CONSTRAINT workflow_samples_expected_outcome_check
  CHECK (expected_outcome IN ('success','error','empty_list'));
CREATE INDEX IF NOT EXISTS idx_workflow_samples_document ON workflow_samples(document_id) WHERE document_id IS NOT NULL;
ALTER TABLE workflow_samples DROP CONSTRAINT IF EXISTS workflow_samples_document_id_fkey;
ALTER TABLE workflow_samples ADD CONSTRAINT workflow_samples_document_id_fkey
  FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE RESTRICT;
`)
	return err
}
