package task_repo

import (
	"errors"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
)

type DocumentRetentionPreview struct {
	Cutoff    time.Time `json:"cutoff"`
	Eligible  int64     `json:"eligible"`
	Protected int64     `json:"protected"`
	Bytes     int64     `json:"bytes"`
}

// Eligible documents have no sample, observation or surviving task reference.
// A scheduled cleanup can use the same predicate after an operator reviews
// this preview in the target environment.
func PreviewDocumentRetention(before time.Time) (DocumentRetentionPreview, error) {
	result := DocumentRetentionPreview{Cutoff: before}
	if before.IsZero() || before.After(time.Now()) {
		return result, errors.New("retention cutoff must be in the past")
	}
	conn := db.Instance().DB().DB
	err := conn.QueryRow(`SELECT COUNT(*),COALESCE(SUM(d.content_size+COALESCE(a.asset_bytes,0)),0)
FROM documents d LEFT JOIN LATERAL(SELECT SUM(content_size) AS asset_bytes FROM document_assets WHERE document_id=d.id) a ON true
WHERE d.created_at<$1 AND NOT EXISTS(SELECT 1 FROM workflow_samples s WHERE s.document_id=d.id)
AND NOT EXISTS(SELECT 1 FROM record_observations o WHERE o.document_id=d.id)
AND NOT EXISTS(SELECT 1 FROM crawl_tasks t WHERE t.output_document_id=d.id OR t.id=d.task_id)`, before).Scan(&result.Eligible, &result.Bytes)
	if err != nil {
		return result, err
	}
	err = conn.QueryRow(`SELECT COUNT(*) FROM documents d WHERE d.created_at<$1 AND (
EXISTS(SELECT 1 FROM workflow_samples s WHERE s.document_id=d.id)
OR EXISTS(SELECT 1 FROM record_observations o WHERE o.document_id=d.id)
OR EXISTS(SELECT 1 FROM crawl_tasks t WHERE t.output_document_id=d.id OR t.id=d.task_id))`, before).Scan(&result.Protected)
	return result, err
}

// CleanupExpiredDocuments removes at most 100 orphan documents per call.
// Row locks serialize this with any new foreign-key reference to the document.
func CleanupExpiredDocuments(before time.Time) (int64, error) {
	return cleanupExpiredDocuments(before, 0)
}

func cleanupExpiredDocuments(before time.Time, onlyID int64) (int64, error) {
	if before.IsZero() || before.After(time.Now()) {
		return 0, errors.New("retention cutoff must be in the past")
	}
	conn := db.Instance().DB().DB
	result, err := conn.Exec(`WITH victims AS (
 SELECT d.id FROM documents d WHERE d.created_at<$1 AND ($2=0 OR d.id=$2)
 AND NOT EXISTS(SELECT 1 FROM workflow_samples s WHERE s.document_id=d.id)
 AND NOT EXISTS(SELECT 1 FROM record_observations o WHERE o.document_id=d.id)
 AND NOT EXISTS(SELECT 1 FROM crawl_tasks t WHERE t.output_document_id=d.id OR t.id=d.task_id)
 ORDER BY d.id LIMIT 100 FOR UPDATE OF d SKIP LOCKED
), removed_assets AS (DELETE FROM document_assets a USING victims v WHERE a.document_id=v.id)
DELETE FROM documents d USING victims v WHERE d.id=v.id`, before, onlyID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
