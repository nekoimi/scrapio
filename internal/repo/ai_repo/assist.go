package ai_repo

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/ai"
	"github.com/nekoimi/scrapio/internal/db"
)

type RuleSuggestion struct {
	ID           int64      `json:"id" xorm:"pk autoincr id"`
	WorkflowID   int64      `json:"workflow_id" xorm:"workflow_id"`
	VersionID    int64      `json:"version_id" xorm:"version_id"`
	SampleID     int64      `json:"sample_id" xorm:"sample_id"`
	RequestID    int64      `json:"request_id" xorm:"request_id"`
	Model        string     `json:"model" xorm:"model"`
	ContentHash  string     `json:"content_hash" xorm:"content_hash"`
	Result       string     `json:"result" xorm:"result"`
	Status       string     `json:"status" xorm:"status"`
	InputTokens  int        `json:"input_tokens" xorm:"input_tokens"`
	OutputTokens int        `json:"output_tokens" xorm:"output_tokens"`
	CreatedAt    time.Time  `json:"created_at" xorm:"created_at"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty" xorm:"reviewed_at"`
}

func (RuleSuggestion) TableName() string { return "ai_rule_suggestions" }

// Reserve consumes one request from the UTC daily allowance before contacting
// the provider. A failed external call still consumes the allowance.
func Reserve(workflowID, versionID, sampleID int64, limit int) (int64, error) {
	if db.Instance() == nil {
		return 0, errors.New("database is not initialized")
	}
	if limit < 1 || limit > 1000 {
		return 0, errors.New("AI daily request limit must be 1–1000")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return 0, err
	}
	defer s.Rollback()
	if _, err := s.Exec("SELECT pg_advisory_xact_lock(?)", int64(0x5343524150494f)); err != nil {
		return 0, err
	}
	var used int64
	rows, err := s.QueryString("SELECT count(*) AS count FROM ai_assist_requests WHERE created_at >= date_trunc('day',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'")
	if err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		_, err = fmtScanInt(rows[0]["count"], &used)
		if err != nil {
			return 0, err
		}
	}
	if used >= int64(limit) {
		return 0, errors.New("AI daily request limit reached")
	}
	rows, err = s.QueryString("INSERT INTO ai_assist_requests(workflow_id,version_id,sample_id,status) VALUES(?,?,?,'reserved') RETURNING id", workflowID, versionID, sampleID)
	if err != nil {
		return 0, err
	}
	var id int64
	_, err = fmtScanInt(rows[0]["id"], &id)
	if err != nil {
		return 0, err
	}
	return id, s.Commit()
}

func fmtScanInt(raw string, target *int64) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	*target = value
	return value, err
}

func FailRequest(id int64) {
	if db.Instance() != nil {
		_, _ = db.Instance().Exec("UPDATE ai_assist_requests SET status='failed',finished_at=NOW() WHERE id=? AND status='reserved'", id)
	}
}

func SaveRuleSuggestion(requestID, workflowID, versionID, sampleID int64, model, contentHash string, result ai.AssistResponse) (*RuleSuggestion, error) {
	if db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 65536 {
		return nil, errors.New("AI suggestion exceeds size limit")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString(`INSERT INTO ai_rule_suggestions(workflow_id,version_id,sample_id,request_id,model,content_hash,result,status,input_tokens,output_tokens)
 VALUES(?,?,?,?,?, ?,?::jsonb,'pending_review',?,?) RETURNING id`, workflowID, versionID, sampleID, requestID, model, contentHash, string(encoded), result.InputTokens, result.OutputTokens)
	if err != nil {
		return nil, err
	}
	if _, err := s.Exec("UPDATE ai_assist_requests SET status='succeeded',finished_at=NOW() WHERE id=?", requestID); err != nil {
		return nil, err
	}
	var id int64
	_, err = fmtScanInt(rows[0]["id"], &id)
	if err != nil {
		return nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return GetRuleSuggestion(id)
}

func GetRuleSuggestion(id int64) (*RuleSuggestion, error) {
	if db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	row := new(RuleSuggestion)
	has, err := db.Instance().ID(id).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, errors.New("AI suggestion not found")
	}
	return row, nil
}
func ListRuleSuggestions(versionID int64) ([]RuleSuggestion, error) {
	rows := make([]RuleSuggestion, 0)
	if db.Instance() == nil {
		return rows, errors.New("database is not initialized")
	}
	if versionID <= 0 {
		return rows, errors.New("version ID is required")
	}
	err := db.Instance().Where("version_id=?", versionID).Desc("created_at", "id").Limit(30).Find(&rows)
	return rows, err
}

func ReviewRuleSuggestion(id, versionID int64, status string) (*RuleSuggestion, error) {
	if status != "accepted" && status != "rejected" {
		return nil, errors.New("invalid review decision")
	}
	if db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	changed, err := s.Exec("UPDATE ai_rule_suggestions SET status=?,reviewed_at=NOW() WHERE id=? AND version_id=? AND status='pending_review'", status, id, versionID)
	if err != nil {
		return nil, err
	}
	affected, err := changed.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, errors.New("suggestion was already reviewed or does not belong to version")
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return GetRuleSuggestion(id)
}
