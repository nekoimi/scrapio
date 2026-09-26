package record_repo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
)

const DirectExportLimit = 1000
const AsyncExportLimit = 100000
const ExportByteLimit = 50 << 20

type ExportJob struct {
	ID         int64      `json:"id"`
	DatasetID  int64      `json:"dataset_id"`
	Status     string     `json:"status"`
	RowCount   int        `json:"row_count"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

// BuildCSV holds a consistent snapshot and enforces both row and byte limits.
func BuildCSV(ctx context.Context, f Filter, maxRows int) ([]byte, int, error) {
	if err := f.Validate(); err != nil {
		return nil, 0, err
	}
	tx, err := db.Instance().DB().DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	rows, err := queryExportRows(ctx, tx, f)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out bytes.Buffer
	out.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(&out)
	if err := writer.Write([]string{"canonical_key", "fields_json", "first_seen_at", "last_seen_at"}); err != nil {
		return nil, 0, err
	}
	count := 0
	for rows.Next() {
		var key string
		var normalized []byte
		var first, last time.Time
		if err := rows.Scan(&key, &normalized, &first, &last); err != nil {
			return nil, 0, err
		}
		count++
		if count > maxRows {
			return nil, 0, errors.New("export row limit exceeded")
		}
		if err := writer.Write([]string{csvSafe(key), string(normalized), first.Format(time.RFC3339), last.Format(time.RFC3339)}); err != nil {
			return nil, 0, err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, 0, err
		}
		if out.Len() > ExportByteLimit {
			return nil, 0, errors.New("export byte limit exceeded")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, 0, err
	}
	return out.Bytes(), count, nil
}

func CreateExportJob(ctx context.Context, f Filter) (*ExportJob, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	n, err := CountFiltered(ctx, f)
	if err != nil {
		return nil, err
	}
	if n > AsyncExportLimit {
		return nil, fmt.Errorf("export exceeds %d rows; narrow the filter", AsyncExportLimit)
	}
	var active int
	if err := db.Instance().DB().DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM record_export_jobs WHERE dataset_id=$1 AND status IN ('queued','running')", f.DatasetID).Scan(&active); err != nil {
		return nil, err
	}
	if active >= 5 {
		return nil, errors.New("dataset already has five pending exports")
	}
	raw, _ := json.Marshal(f)
	job := new(ExportJob)
	job.DatasetID = f.DatasetID
	job.RowCount = int(n)
	err = db.Instance().DB().DB.QueryRowContext(ctx, `INSERT INTO record_export_jobs(dataset_id,filter,status,row_count) VALUES($1,$2::jsonb,'queued',$3) RETURNING id,created_at`, f.DatasetID, string(raw), n).Scan(&job.ID, &job.CreatedAt)
	job.Status = "queued"
	return job, err
}

func GetExportJob(id int64) (*ExportJob, error) {
	if id <= 0 {
		return nil, errors.New("invalid export job")
	}
	j := new(ExportJob)
	err := db.Instance().DB().DB.QueryRow(`SELECT id,dataset_id,status,row_count,error,created_at,finished_at FROM record_export_jobs WHERE id=$1`, id).Scan(&j.ID, &j.DatasetID, &j.Status, &j.RowCount, &j.Error, &j.CreatedAt, &j.FinishedAt)
	return j, err
}
func ExportContent(id int64) (int64, []byte, error) {
	var datasetID int64
	var content []byte
	err := db.Instance().DB().DB.QueryRow("SELECT dataset_id,content FROM record_export_jobs WHERE id=$1 AND status='ready' AND created_at>NOW()-INTERVAL '7 days'", id).Scan(&datasetID, &content)
	return datasetID, content, err
}

func RecoverExports() error {
	_, err := db.Instance().DB().DB.Exec("UPDATE record_export_jobs SET status='queued' WHERE status='running'")
	return err
}
func CleanupExports() error {
	_, err := db.Instance().DB().DB.Exec("DELETE FROM record_export_jobs WHERE created_at < NOW()-INTERVAL '7 days'")
	return err
}
func ProcessNextExport(ctx context.Context) (bool, error) {
	conn := db.Instance().DB().DB
	var id, datasetID int64
	var raw []byte
	err := conn.QueryRowContext(ctx, `UPDATE record_export_jobs SET status='running' WHERE id=(SELECT id FROM record_export_jobs WHERE status='queued' ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id,dataset_id,filter`).Scan(&id, &datasetID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var f Filter
	if err := json.Unmarshal(raw, &f); err != nil {
		return true, finishExport(id, nil, 0, err)
	}
	if f.DatasetID != datasetID {
		return true, finishExport(id, nil, 0, errors.New("export dataset mismatch"))
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	data, n, err := BuildCSV(ctx, f, AsyncExportLimit)
	if parent.Err() != nil {
		return true, nil
	} // Startup recovery requeues this job.
	return true, finishExport(id, data, n, err)
}
func finishExport(id int64, data []byte, n int, exportErr error) error {
	status, reason := "ready", ""
	if exportErr != nil {
		status, reason = "failed", exportErr.Error()
		data = nil
	}
	_, err := db.Instance().DB().DB.Exec("UPDATE record_export_jobs SET status=$1,content=$2,row_count=$3,error=$4,finished_at=NOW() WHERE id=$5", status, data, n, reason, id)
	return err
}
