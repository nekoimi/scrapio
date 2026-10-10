package migrate

import "xorm.io/xorm"

type v22Quality struct{}

func init()                        { registerMigrate(new(v22Quality)) }
func (*v22Quality) Version() int64 { return 2026_10_10_011 }
func (*v22Quality) Desc() string   { return "v2.2 异常策略、正式证据与恢复待办" }
func (*v22Quality) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
CREATE TABLE IF NOT EXISTS v22_quality_policies (
 collector_id BIGINT PRIMARY KEY REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT, revision INT NOT NULL,
 policy JSONB NOT NULL, baseline JSONB NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE IF NOT EXISTS v22_quality_evaluations (
 run_id VARCHAR(36) PRIMARY KEY REFERENCES v22_runs(id) ON DELETE RESTRICT,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 policy_revision INT NOT NULL, facts JSONB NOT NULL, result JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_v22_quality_eval ON v22_quality_evaluations(owner_id,collector_id,created_at DESC,run_id);
CREATE INDEX IF NOT EXISTS idx_v22_quality_terminal ON v22_runs(finished_at,id) WHERE status NOT IN ('queued','running') AND finished_at IS NOT NULL;
CREATE TABLE IF NOT EXISTS v22_quality_issues (
 id VARCHAR(36) PRIMARY KEY, owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 code VARCHAR(64) NOT NULL, category VARCHAR(32) NOT NULL, message TEXT NOT NULL,
 status VARCHAR(20) NOT NULL CHECK(status IN ('open','ready','resolved')), revision INT NOT NULL DEFAULT 1,
 occurrences INT NOT NULL DEFAULT 1, recovery_streak INT NOT NULL DEFAULT 0,
 first_run_id VARCHAR(36) NOT NULL DEFAULT '', last_run_id VARCHAR(36) NOT NULL DEFAULT '', recovery_run_id VARCHAR(36) NOT NULL DEFAULT '',
 evidence JSONB NOT NULL DEFAULT '{}', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), resolved_at TIMESTAMPTZ,
 UNIQUE(owner_id,collector_id,code));
CREATE INDEX IF NOT EXISTS idx_v22_quality_issues ON v22_quality_issues(owner_id,status,collector_id,id);
`)
	return err
}
