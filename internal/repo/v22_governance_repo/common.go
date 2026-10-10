package v22_governance_repo

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/governance"
	"xorm.io/xorm"
)

var (
	ErrInvalid  = errors.New("invalid governance request")
	ErrConflict = errors.New("preview is expired or dependencies changed; preview again")
	ErrNotFound = errors.New("governance resource not found")
	ErrCapacity = errors.New("preview asset capacity reached; clean unprotected inputs or raise limit")
)

func begin(ctx context.Context) (*xorm.Session, error) {
	s := db.Instance().NewSession().Context(ctx)
	if err := s.Begin(); err != nil {
		s.Close()
		return nil, err
	}
	// Bounded SQL even if the client disconnects or another transaction owns a lock.
	if _, err := s.Exec("SET LOCAL statement_timeout = '4000ms'"); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func number(rows []map[string]string, key string) int64 {
	if len(rows) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(rows[0][key], 10, 64)
	return v
}
func Record(s *xorm.Session, owner int64, action, id string, details governance.SafeDetails) error {
	_, err := s.Exec("INSERT INTO v22_operation_logs(owner_id,actor_id,action,resource_id,status,details,finished_at) VALUES(?,?,?,?,'committed',?::jsonb,?)", owner, owner, action, id, details.JSON(), time.Now().UTC())
	return err
}

func StartOperation(ctx context.Context, owner int64, requestID, action, id string) (int64, error) {
	rows, err := db.Instance().Context(ctx).QueryString("INSERT INTO v22_operation_logs(owner_id,actor_id,request_id,action,resource_id,status) VALUES(?,?,?,?,?,'started') RETURNING id", owner, owner, requestID, action, id)
	return number(rows, "id"), err
}
func FinishOperation(ctx context.Context, owner, id int64, status int) error {
	state := "rejected"
	if status >= 200 && status < 300 {
		state = "succeeded"
	} else if status >= 500 {
		state = "outcome_unknown"
	}
	_, err := db.Instance().Context(ctx).Exec("UPDATE v22_operation_logs SET status=?,http_status=?,finished_at=? WHERE id=? AND owner_id=?", state, status, time.Now().UTC(), id, owner)
	return err
}
func Operations(ctx context.Context, owner, before int64, limit int) ([]map[string]string, string, error) {
	if before < 0 || limit < 1 || limit > 50 {
		return nil, "", ErrInvalid
	}
	rows, err := db.Instance().Context(ctx).QueryString(`SELECT l.id::text,l.actor_id::text,a.username,l.request_id,l.action,l.resource_id,l.status,l.http_status::text,l.details::text,l.created_at::text,l.finished_at::text
 FROM v22_operation_logs l JOIN admin a ON a.id=l.actor_id WHERE l.owner_id=? AND (?=0 OR l.id<?) ORDER BY l.id DESC LIMIT ?`, owner, before, before, limit+1)
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1]["id"]
	}
	if rows == nil {
		rows = []map[string]string{}
	}
	return rows, next, err
}
