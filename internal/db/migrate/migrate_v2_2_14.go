package migrate

import "xorm.io/xorm"

type v22Schedules struct{}

func init()                          { registerMigrate(new(v22Schedules)) }
func (*v22Schedules) Version() int64 { return 2026_10_10_006 }
func (*v22Schedules) Desc() string {
	return "v2.2 独立定时计划、触发日志和方案 API 凭据"
}
func (*v22Schedules) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
ALTER TABLE v22_runs DROP CONSTRAINT IF EXISTS v22_runs_trigger_source_check;
ALTER TABLE v22_runs ADD CONSTRAINT v22_runs_trigger_source_check CHECK(trigger_source IN ('manual','schedule','api'));
CREATE TABLE IF NOT EXISTS v22_schedules (
 collector_id BIGINT PRIMARY KEY REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 revision INTEGER NOT NULL, enabled BOOLEAN NOT NULL,
 cron VARCHAR(128) NOT NULL, timezone VARCHAR(64) NOT NULL,
 overlap VARCHAR(16) NOT NULL CHECK(overlap IN ('skip','queue')), input JSONB NOT NULL,
 next_at TIMESTAMPTZ,last_decision VARCHAR(64) NOT NULL DEFAULT '',last_run_id VARCHAR(36) NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), CHECK(enabled=(next_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_v22_schedules_due ON v22_schedules(next_at) WHERE enabled;
CREATE TABLE IF NOT EXISTS v22_schedule_events (
 id BIGSERIAL PRIMARY KEY,collector_id BIGINT NOT NULL REFERENCES v22_schedules(collector_id) ON DELETE RESTRICT,
 revision INTEGER NOT NULL,decision VARCHAR(64) NOT NULL,due_at TIMESTAMPTZ,run_id VARCHAR(36),
 coalesced BOOLEAN NOT NULL DEFAULT FALSE,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_v22_schedule_events ON v22_schedule_events(collector_id,id DESC);
CREATE TABLE IF NOT EXISTS v22_api_keys (
 id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id) ON DELETE RESTRICT,
 collector_id BIGINT NOT NULL REFERENCES v22_collectors(id) ON DELETE RESTRICT,
 name VARCHAR(80) NOT NULL,token_hash VARCHAR(64) NOT NULL UNIQUE,input JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),revoked_at TIMESTAMPTZ
);
`)
	return err
}
