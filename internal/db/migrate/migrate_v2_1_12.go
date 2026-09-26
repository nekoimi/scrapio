package migrate

import "xorm.io/xorm"

type qualityThresholds struct{}

func init()                               { registerMigrate(new(qualityThresholds)) }
func (*qualityThresholds) Version() int64 { return 2026_09_26_009 }
func (*qualityThresholds) Desc() string   { return "采集器质量阈值" }
func (*qualityThresholds) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`CREATE TABLE IF NOT EXISTS workflow_quality_thresholds (
 workflow_id BIGINT PRIMARY KEY REFERENCES workflows(id) ON DELETE CASCADE,
 min_key_rate DOUBLE PRECISION NOT NULL DEFAULT 0.95 CHECK (min_key_rate BETWEEN 0 AND 1),
 max_required_missing_rate DOUBLE PRECISION NOT NULL DEFAULT 0.05 CHECK (max_required_missing_rate BETWEEN 0 AND 1),
 max_anomaly_rate DOUBLE PRECISION NOT NULL DEFAULT 0.10 CHECK (max_anomaly_rate BETWEEN 0 AND 1),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`)
	return err
}
