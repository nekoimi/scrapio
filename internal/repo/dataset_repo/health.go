package dataset_repo

import (
	"database/sql"
	"errors"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
)

type DatasetHealth struct {
	DatasetID       int64      `json:"dataset_id"`
	Name            string     `json:"name"`
	Records         int64      `json:"records"`
	Created7d       int64      `json:"created_7d"`
	Updated7d       int64      `json:"updated_7d"`
	LastEffectiveAt *time.Time `json:"last_effective_at,omitempty"`
}
type CollectorHealth struct {
	WorkflowID    int64      `json:"workflow_id"`
	Name          string     `json:"name"`
	DatasetID     *int64     `json:"dataset_id,omitempty"`
	LastRunID     *int64     `json:"last_run_id,omitempty"`
	LastRunStatus string     `json:"last_run_status"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	Issue         string     `json:"issue,omitempty"`
}
type ProjectHealth struct {
	ProjectID       int64             `json:"project_id"`
	LastEffectiveAt *time.Time        `json:"last_effective_at,omitempty"`
	Created7d       int64             `json:"created_7d"`
	Updated7d       int64             `json:"updated_7d"`
	Datasets        []DatasetHealth   `json:"datasets"`
	Collectors      []CollectorHealth `json:"collectors"`
	Issues          int               `json:"issues"`
}

func Health(projectID int64) (*ProjectHealth, error) {
	if projectID <= 0 {
		return nil, errors.New("project_id is required")
	}
	conn := db.Instance().DB().DB
	var exists bool
	if err := conn.QueryRow("SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", projectID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("project not found")
	}
	out := &ProjectHealth{ProjectID: projectID, Datasets: make([]DatasetHealth, 0), Collectors: make([]CollectorHealth, 0)}
	rows, err := conn.Query(`SELECT d.id,d.name,(SELECT COUNT(*) FROM records r WHERE r.dataset_id=d.id AND r.status='active'),
COUNT(rv.id) FILTER(WHERE o.decision='created' AND rv.created_at>=NOW()-INTERVAL '7 days'),
COUNT(rv.id) FILTER(WHERE o.decision='updated' AND rv.created_at>=NOW()-INTERVAL '7 days'),MAX(rv.created_at)
FROM datasets d LEFT JOIN record_observations o ON o.dataset_id=d.id AND o.decision IN ('created','updated')
LEFT JOIN record_revisions rv ON rv.observation_id=o.id WHERE d.project_id=$1 GROUP BY d.id,d.name ORDER BY d.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DatasetHealth
		var last sql.NullTime
		if err := rows.Scan(&d.DatasetID, &d.Name, &d.Records, &d.Created7d, &d.Updated7d, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			d.LastEffectiveAt = &last.Time
			if out.LastEffectiveAt == nil || last.Time.After(*out.LastEffectiveAt) {
				out.LastEffectiveAt = &last.Time
			}
		}
		out.Created7d += d.Created7d
		out.Updated7d += d.Updated7d
		out.Datasets = append(out.Datasets, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	rows, err = conn.Query(`SELECT w.id,w.name,w.dataset_id,w.enabled,w.published_version_id,
latest.id,latest.status,latest.created_at,
(SELECT MAX(COALESCE(r.finished_at,r.created_at)) FROM workflow_runs r WHERE r.workflow_id=w.id AND r.status='succeeded'),
COALESCE(s.enabled,false)
FROM workflows w LEFT JOIN LATERAL(SELECT id,status,created_at FROM workflow_runs WHERE workflow_id=w.id ORDER BY id DESC LIMIT 1) latest ON true
LEFT JOIN workflow_schedules s ON s.workflow_id=w.id WHERE w.project_id=$1 ORDER BY w.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c CollectorHealth
		var datasetID, published, lastID sql.NullInt64
		var enabled, scheduled bool
		var status sql.NullString
		var lastRun, lastSuccess sql.NullTime
		if err := rows.Scan(&c.WorkflowID, &c.Name, &datasetID, &enabled, &published, &lastID, &status, &lastRun, &lastSuccess, &scheduled); err != nil {
			return nil, err
		}
		if datasetID.Valid {
			c.DatasetID = &datasetID.Int64
		}
		if lastID.Valid {
			c.LastRunID = &lastID.Int64
		}
		c.LastRunStatus = status.String
		if lastRun.Valid {
			c.LastRunAt = &lastRun.Time
		}
		if lastSuccess.Valid {
			c.LastSuccessAt = &lastSuccess.Time
		}
		switch {
		case !enabled:
			c.Issue = "采集器已停用"
		case !published.Valid:
			c.Issue = "尚未发布"
		case !lastID.Valid:
			c.Issue = "已发布但从未运行"
		case status.String == "failed" || status.String == "cancelled" || status.String == "partial":
			c.Issue = "最近运行" + status.String
		case scheduled && (!lastSuccess.Valid || time.Since(lastSuccess.Time) > 7*24*time.Hour):
			c.Issue = "定时采集超过 7 天没有成功运行"
		}
		if c.Issue != "" {
			out.Issues++
		}
		out.Collectors = append(out.Collectors, c)
	}
	return out, rows.Err()
}
