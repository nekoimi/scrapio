package task_repo

import (
	"database/sql"
	"errors"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
)

// RunInspection is rebuilt from durable task, attempt and observation rows.
// It also remains useful while the run is in progress.
type RunInspection struct {
	Tasks            int64    `json:"tasks"`
	PagesVisited     int64    `json:"pages_visited"`
	PagesFailed      int64    `json:"pages_failed"`
	PagesLimited     int64    `json:"pages_limited"`
	Discovered       int64    `json:"discovered"`
	Candidates       int64    `json:"candidates"`
	Created          int64    `json:"created"`
	Updated          int64    `json:"updated"`
	Unchanged        int64    `json:"unchanged"`
	LastError        string   `json:"last_error,omitempty"`
	LastFailedTaskID *int64   `json:"last_failed_task_id,omitempty"`
	StopReasons      []string `json:"stop_reasons"`
}

func InspectRun(runID int64) (*RunInspection, error) {
	if runID <= 0 {
		return nil, errors.New("run id is required")
	}
	conn := db.Instance().DB().DB
	result := &RunInspection{StopReasons: []string{}}
	err := conn.QueryRow(`SELECT COUNT(*),COUNT(*) FILTER(WHERE output_document_id IS NOT NULL),
	COUNT(*) FILTER(WHERE status IN ('failed','dead_letter')),
	COUNT(*) FILTER(WHERE status='limited')
	FROM crawl_tasks WHERE run_id=$1`, runID).Scan(&result.Tasks, &result.PagesVisited, &result.PagesFailed, &result.PagesLimited)
	if err != nil {
		return nil, err
	}
	err = conn.QueryRow(`SELECT COALESCE(SUM(COALESCE((a.response_snapshot->>'discovered_count')::bigint,0)),0),
	COALESCE(SUM(COALESCE((a.response_snapshot->>'record_count')::bigint,0)),0)
	FROM crawl_tasks t LEFT JOIN LATERAL (
	 SELECT response_snapshot FROM task_attempts WHERE task_id=t.id AND status='succeeded'
	 ORDER BY attempt_no DESC LIMIT 1
	) a ON true WHERE t.run_id=$1 AND t.status='succeeded'`, runID).Scan(&result.Discovered, &result.Candidates)
	if err != nil {
		return nil, err
	}
	err = conn.QueryRow(`SELECT COUNT(*) FILTER(WHERE decision='created'),COUNT(*) FILTER(WHERE decision='updated'),
	COUNT(*) FILTER(WHERE decision='unchanged') FROM record_observations WHERE run_id=$1`, runID).
		Scan(&result.Created, &result.Updated, &result.Unchanged)
	if err != nil {
		return nil, err
	}
	// A persist failure may have candidates but no observation rows. Count the
	// attempted batch from its structured failure snapshot, once per task.
	var failedCandidates int64
	err = conn.QueryRow(`SELECT COALESCE(SUM(COALESCE((a.response_snapshot->>'candidate_count')::bigint,0)),0)
	FROM crawl_tasks t LEFT JOIN LATERAL (
	 SELECT response_snapshot FROM task_attempts WHERE task_id=t.id AND status='failed'
	 ORDER BY attempt_no DESC LIMIT 1
	) a ON true WHERE t.run_id=$1 AND t.status IN ('failed','dead_letter')`, runID).Scan(&failedCandidates)
	if err != nil {
		return nil, err
	}
	result.Candidates += failedCandidates
	if observed := result.Created + result.Updated + result.Unchanged; observed > result.Candidates {
		result.Candidates = observed
	}
	var id sql.NullInt64
	var message sql.NullString
	err = conn.QueryRow(`SELECT id,error_message FROM crawl_tasks WHERE run_id=$1 AND status IN ('dead_letter','failed') ORDER BY id DESC LIMIT 1`, runID).Scan(&id, &message)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if id.Valid {
		result.LastFailedTaskID = &id.Int64
		result.LastError = message.String
	}
	rows, err := conn.Query(`SELECT reason FROM (
	 SELECT reason FROM run_limit_events WHERE run_id=$1
	 UNION SELECT a.response_snapshot->>'stop_reason' AS reason
	 FROM task_attempts a JOIN crawl_tasks t ON t.id=a.task_id
	 WHERE t.run_id=$1 AND a.status='succeeded' AND a.response_snapshot->>'stop_reason' <> ''
	) reasons WHERE reason IS NOT NULL ORDER BY reason`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			return nil, err
		}
		result.StopReasons = append(result.StopReasons, reason)
	}
	return result, rows.Err()
}

func ListRunTasks(runID int64, page, size int) ([]table.CrawlTask, int64, error) {
	if runID <= 0 {
		return nil, 0, errors.New("run id is required")
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 100
	}
	if page > 100000 {
		return nil, 0, errors.New("page is too large")
	}
	var total int64
	conn := db.Instance().DB().DB
	if err := conn.QueryRow("SELECT COUNT(*) FROM crawl_tasks WHERE run_id=$1", runID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows := make([]table.CrawlTask, 0)
	err := db.Instance().Where("run_id=?", runID).Asc("id").Limit(size, (page-1)*size).Find(&rows)
	return rows, total, err
}

func ListTaskAttempts(taskID int64, page, size int) ([]table.TaskAttempt, int64, error) {
	if taskID <= 0 {
		return nil, 0, errors.New("task id is required")
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 20 {
		size = 20
	}
	if page > 100000 {
		return nil, 0, errors.New("page is too large")
	}
	var total int64
	if err := db.Instance().DB().DB.QueryRow("SELECT COUNT(*) FROM task_attempts WHERE task_id=$1", taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows := make([]table.TaskAttempt, 0)
	err := db.Instance().Where("task_id=?", taskID).Desc("attempt_no").Limit(size, (page-1)*size).Find(&rows)
	return rows, total, err
}
