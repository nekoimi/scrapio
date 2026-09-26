package dataset_repo

import (
	"database/sql"
	"errors"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
)

type QualityThreshold struct {
	WorkflowID             int64   `json:"workflow_id"`
	MinKeyRate             float64 `json:"min_key_rate"`
	MaxRequiredMissingRate float64 `json:"max_required_missing_rate"`
	MaxAnomalyRate         float64 `json:"max_anomaly_rate"`
}

type QualityRun struct {
	RunID               int64     `json:"run_id"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	Pages               int64     `json:"pages"`
	RequiredMissing     int64     `json:"required_missing"`
	Anomalies           int64     `json:"anomalies"`
	Records             int64     `json:"records"`
	RequiredMissingRate float64   `json:"required_missing_rate"`
	KeyRate             float64   `json:"key_rate"`
	AnomalyRate         float64   `json:"anomaly_rate"`
}

type QualityTrend struct {
	WorkflowID  int64            `json:"workflow_id"`
	DatasetID   *int64           `json:"dataset_id,omitempty"`
	HasBaseline bool             `json:"has_baseline"`
	Threshold   QualityThreshold `json:"threshold"`
	Runs        []QualityRun     `json:"runs"`
	Issue       string           `json:"issue,omitempty"`
}

func Quality(workflowID int64) (*QualityTrend, error) {
	if workflowID <= 0 {
		return nil, errors.New("workflow_id is required")
	}
	conn := db.Instance().DB().DB
	result := &QualityTrend{WorkflowID: workflowID, Runs: []QualityRun{}, Threshold: QualityThreshold{WorkflowID: workflowID, MinKeyRate: 0.95, MaxRequiredMissingRate: 0.05, MaxAnomalyRate: 0.10}}
	var exists bool
	if err := conn.QueryRow("SELECT EXISTS(SELECT 1 FROM workflows WHERE id=$1)", workflowID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("workflow not found")
	}
	if err := conn.QueryRow("SELECT dataset_id FROM workflows WHERE id=$1", workflowID).Scan(&result.DatasetID); err != nil {
		return nil, err
	}
	if err := conn.QueryRow(`SELECT min_key_rate,max_required_missing_rate,max_anomaly_rate FROM workflow_quality_thresholds WHERE workflow_id=$1`, workflowID).Scan(&result.Threshold.MinKeyRate, &result.Threshold.MaxRequiredMissingRate, &result.Threshold.MaxAnomalyRate); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := conn.Query(`SELECT r.id,r.status,r.created_at,
	 COUNT(t.id) FILTER(WHERE t.step_name IN ('trigger','detail') AND t.task_type='workflow'),
	 COUNT(t.id) FILTER(WHERE t.status IN ('failed','dead_letter') AND t.error_message ILIKE '%required field%'),
	 COUNT(t.id) FILTER(WHERE t.status IN ('failed','dead_letter') AND (t.error_message ILIKE '%invalid%' OR t.error_message ILIKE '%conflict%')),
	 COALESCE(SUM(CASE WHEN t.status='succeeded' THEN COALESCE((a.response_snapshot->>'record_count')::bigint,0) ELSE 0 END),0)
	 FROM (SELECT id,status,created_at FROM workflow_runs WHERE workflow_id=$1 AND status NOT IN ('queued','running') ORDER BY id DESC LIMIT 10) r
	 LEFT JOIN crawl_tasks t ON t.run_id=r.id
	 LEFT JOIN LATERAL (SELECT response_snapshot FROM task_attempts WHERE task_id=t.id AND status='succeeded' ORDER BY attempt_no DESC LIMIT 1) a ON true
	 GROUP BY r.id,r.status,r.created_at ORDER BY r.id DESC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var run QualityRun
		if err := rows.Scan(&run.RunID, &run.Status, &run.CreatedAt, &run.Pages, &run.RequiredMissing, &run.Anomalies, &run.Records); err != nil {
			return nil, err
		}
		if run.Pages > 0 {
			run.RequiredMissingRate = float64(run.RequiredMissing) / float64(run.Pages)
			run.AnomalyRate = float64(run.Anomalies) / float64(run.Pages)
		}
		if total := run.Records + run.RequiredMissing; total > 0 {
			run.KeyRate = float64(run.Records) / float64(total)
		}
		result.Runs = append(result.Runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, run := range result.Runs {
		if run.Records+run.RequiredMissing > 0 {
			result.HasBaseline = true
			break
		}
	}
	if result.HasBaseline && len(result.Runs) > 0 {
		latest := result.Runs[0]
		switch {
		case latest.RequiredMissingRate > result.Threshold.MaxRequiredMissingRate:
			result.Issue = "必填字段缺失率超阈值"
		case latest.AnomalyRate > result.Threshold.MaxAnomalyRate:
			result.Issue = "异常页面率超阈值"
		case latest.Records+latest.RequiredMissing > 0 && latest.KeyRate < result.Threshold.MinKeyRate:
			result.Issue = "唯一键生成率低于阈值"
		}
	}
	return result, nil
}

func SaveQualityThreshold(value QualityThreshold) error {
	if value.WorkflowID <= 0 || value.MinKeyRate < 0 || value.MinKeyRate > 1 || value.MaxRequiredMissingRate < 0 || value.MaxRequiredMissingRate > 1 || value.MaxAnomalyRate < 0 || value.MaxAnomalyRate > 1 {
		return errors.New("quality thresholds must be between 0 and 1")
	}
	result, err := db.Instance().DB().DB.Exec(`INSERT INTO workflow_quality_thresholds(workflow_id,min_key_rate,max_required_missing_rate,max_anomaly_rate) VALUES($1,$2,$3,$4)
	 ON CONFLICT(workflow_id) DO UPDATE SET min_key_rate=EXCLUDED.min_key_rate,max_required_missing_rate=EXCLUDED.max_required_missing_rate,max_anomaly_rate=EXCLUDED.max_anomaly_rate,updated_at=NOW()`, value.WorkflowID, value.MinKeyRate, value.MaxRequiredMissingRate, value.MaxAnomalyRate)
	_ = result
	return err
}
