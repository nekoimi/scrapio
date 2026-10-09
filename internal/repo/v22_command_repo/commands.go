package v22_command_repo

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_session_repo"
)

var ErrNotFound = errors.New("command not found")
var ErrBusy = errors.New("another command is pending or uncertain; query its receipt or reopen the session")
var ErrIdempotencyConflict = errors.New("idempotency key was already used for another command")

func Create(ownerID int64, sessionID, key string, input editor.Command) (*table.V22EditorCommand, bool, error) {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, false, err
	}
	defer s.Rollback()
	locks, err := s.QueryString("SELECT id FROM v22_browser_sessions WHERE id = ? AND owner_id = ? FOR UPDATE", sessionID, ownerID)
	if err != nil {
		return nil, false, err
	}
	if len(locks) == 0 {
		return nil, false, v22_session_repo.ErrNotFound
	}
	prior := new(table.V22EditorCommand)
	if has, err := s.Where("owner_id = ? AND session_id = ? AND idempotency_key = ?", ownerID, sessionID, key).Get(prior); err != nil {
		return nil, false, err
	} else if has {
		if prior.Fingerprint != input.Fingerprint() {
			return nil, false, ErrIdempotencyConflict
		}
		return prior, false, nil
	}
	session := new(table.V22BrowserSession)
	if has, err := s.ID(sessionID).Get(session); err != nil {
		return nil, false, err
	} else if !has {
		return nil, false, v22_session_repo.ErrNotFound
	}
	if session.Status != "ready" || !session.ExpiresAt.After(time.Now()) {
		return nil, false, v22_session_repo.ErrSessionGone
	}
	collector := new(table.V22Collector)
	if has, err := s.Where("id = ? AND owner_id = ? AND status <> 'archived'", session.CollectorId, ownerID).Get(collector); err != nil {
		return nil, false, err
	} else if !has {
		return nil, false, v22_session_repo.ErrNotFound
	}
	if input.ExpectedRevision != collector.Revision || collector.EntryURL != session.TargetURL {
		return nil, false, &v22_collector_repo.RevisionConflict{Latest: collector}
	}
	if count, err := s.Where("session_id = ? AND status IN ('queued','running','uncertain')", sessionID).Count(new(table.V22EditorCommand)); err != nil {
		return nil, false, err
	} else if count > 0 {
		return nil, false, ErrBusy
	}
	if input.PageStateID != session.PageStateId {
		return nil, false, errors.New("STALE_PAGE_STATE")
	}
	if input.Record {
		var definition map[string]any
		if err := json.Unmarshal([]byte(collector.Definition), &definition); err != nil {
			return nil, false, err
		}
		steps, ok := definition["steps"].([]any)
		if !ok || len(steps) >= 100 {
			return nil, false, errors.New("recording supports at most 100 steps")
		}
		for _, item := range steps {
			if step, ok := item.(map[string]any); ok && step["step_id"] == input.StepID {
				return nil, false, errors.New("step_id already exists")
			}
		}
	}
	now := time.Now()
	row := &table.V22EditorCommand{Id: uuid.NewString(), OwnerId: ownerID, SessionId: sessionID, CollectorId: collector.Id, IdempotencyKey: key, Fingerprint: input.Fingerprint(), Request: input.Journal(), Status: "queued", Result: "{}", CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(45 * time.Second)}
	if _, err := s.InsertOne(row); err != nil {
		return nil, false, err
	}
	if _, err := s.ID(sessionID).Cols("draft_revision").Update(&table.V22BrowserSession{DraftRevision: collector.Revision}); err != nil {
		return nil, false, err
	}
	if err := s.Commit(); err != nil {
		return nil, false, err
	}
	return row, true, nil
}

func Get(ownerID int64, sessionID, commandID string) (*table.V22EditorCommand, error) {
	row := new(table.V22EditorCommand)
	has, err := db.Instance().Where("id = ? AND owner_id = ? AND session_id = ?", commandID, ownerID, sessionID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}

func List(ownerID int64, sessionID string) ([]table.V22EditorCommand, error) {
	rows := make([]table.V22EditorCommand, 0)
	err := db.Instance().Where("owner_id = ? AND session_id = ?", ownerID, sessionID).Desc("created_at").Limit(50).Find(&rows)
	return rows, err
}

func Start(row *table.V22EditorCommand) (bool, error) {
	n, err := db.Instance().Where("id = ? AND status = 'queued'", row.Id).Cols("status", "updated_at").Update(&table.V22EditorCommand{Status: "running", UpdatedAt: time.Now()})
	return n == 1, err
}

func Complete(row *table.V22EditorCommand, input editor.Command, result drission_rod.EditorCommandResult, allowRecording bool) error {
	if result.Status != "succeeded" && result.Status != "failed" && result.Status != "uncertain" {
		return errors.New("invalid command result")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	// All command mutations lock session before command/collector.
	if _, err := s.QueryString("SELECT id FROM v22_browser_sessions WHERE id = ? FOR UPDATE", row.SessionId); err != nil {
		return err
	}
	if _, err := s.QueryString("SELECT id FROM v22_editor_commands WHERE id = ? FOR UPDATE", row.Id); err != nil {
		return err
	}
	current := new(table.V22EditorCommand)
	has, err := s.ID(row.Id).Get(current)
	if err != nil {
		return err
	}
	if !has {
		return ErrNotFound
	}
	if current.Status == "succeeded" || current.Status == "failed" {
		return nil
	}
	session := new(table.V22BrowserSession)
	has, err = s.ID(row.SessionId).Get(session)
	if err != nil {
		return err
	}
	if !has {
		return v22_session_repo.ErrNotFound
	}
	if input.Record && result.Status == "succeeded" {
		result.RecordingStatus = "conflict"
		result.RecordingError = "动作已执行，草稿已变化或会话已结束；未自动加入步骤，请检查后手动处理"
		if !allowRecording {
			result.RecordingError = "回执已恢复；未自动录制，请检查页面并手动加入步骤"
		} else if session.Status == "ready" && session.ExpiresAt.After(time.Now()) {
			if _, err := s.QueryString("SELECT id FROM v22_collectors WHERE id = ? FOR UPDATE", row.CollectorId); err != nil {
				return err
			}
			collector := new(table.V22Collector)
			has, err := s.Where("id = ? AND owner_id = ?", row.CollectorId, row.OwnerId).Get(collector)
			if err != nil {
				return err
			}
			if has && collector.Status != "archived" && collector.Revision == input.ExpectedRevision {
				var definition map[string]any
				if err := json.Unmarshal([]byte(collector.Definition), &definition); err != nil {
					return err
				}
				steps, ok := definition["steps"].([]any)
				if !ok {
					return errors.New("invalid steps")
				}
				if result.Locator != nil {
					input.Locator = result.Locator
					input.Position = nil
				}
				steps = append(steps, input.Step())
				definition["steps"] = steps
				raw, err := json.Marshal(definition)
				if err != nil {
					return err
				}
				collector.Definition = string(raw)
				collector.Revision++
				collector.UpdatedAt = time.Now()
				collector.UpdatedBy = row.OwnerId
				collector.ValidatedRevision = 0
				collector.ValidationSummary = `{"valid":false,"errors":[]}`
				if _, err := s.ID(collector.Id).Cols("definition", "revision", "updated_at", "updated_by", "validated_revision", "validation_summary").Update(collector); err != nil {
					return err
				}
				if _, err := s.ID(session.Id).Cols("draft_revision").Update(&table.V22BrowserSession{DraftRevision: collector.Revision}); err != nil {
					return err
				}
				result.RecordingStatus = "recorded"
				result.RecordingError = ""
				result.RecordedRevision = collector.Revision
			}
		}
	}
	// Refresh state from a known result; a closed/expired tab stays terminal.
	if result.AfterPageStateID != "" && session.Status == "ready" {
		if _, err := s.ID(session.Id).Cols("page_state_id", "current_url", "updated_at").Update(&table.V22BrowserSession{PageStateId: result.AfterPageStateID, CurrentURL: result.FinalURL, UpdatedAt: time.Now()}); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if _, err := s.ID(row.Id).Cols("status", "result", "updated_at").Update(&table.V22EditorCommand{Status: result.Status, Result: string(raw), UpdatedAt: time.Now()}); err != nil {
		return err
	}
	return s.Commit()
}

func CreateCheckpoint(ownerID, id int64, revision int, name, through string) (*table.V22EditorCheckpoint, error) {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	locks, err := s.QueryString("SELECT id FROM v22_collectors WHERE id = ? AND owner_id = ? AND status <> 'archived' FOR UPDATE", id, ownerID)
	if err != nil {
		return nil, err
	}
	if len(locks) == 0 {
		return nil, v22_session_repo.ErrNotFound
	}
	collector := new(table.V22Collector)
	if _, err := s.ID(id).Get(collector); err != nil {
		return nil, err
	}
	if collector.Revision != revision {
		return nil, &v22_collector_repo.RevisionConflict{Latest: collector}
	}
	var definition map[string]any
	if err := json.Unmarshal([]byte(collector.Definition), &definition); err != nil {
		return nil, err
	}
	steps, ok := definition["steps"].([]any)
	if !ok || len(steps) > 100 {
		return nil, errors.New("checkpoint steps exceed limit")
	}
	selected := make([]any, 0)
	found := false
	seen := map[string]bool{}
	for _, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("invalid step")
		}
		command, err := editor.FromStep(step)
		if err != nil {
			return nil, err
		}
		if seen[command.StepID] {
			return nil, errors.New("duplicate step_id")
		}
		seen[command.StepID] = true
		selected = append(selected, step)
		if command.StepID == through {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("through_step_id must reference an executable step")
	}
	if count, err := s.Where("owner_id = ? AND collector_id = ?", ownerID, id).Count(new(table.V22EditorCheckpoint)); err != nil {
		return nil, err
	} else if count >= 100 {
		return nil, errors.New("checkpoint limit reached; delete unused checkpoints")
	}
	snapshot, _ := json.Marshal(map[string]any{"definition_version": 1, "entry_url": collector.EntryURL, "steps": selected})
	row := &table.V22EditorCheckpoint{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: id, Name: name, DraftRevision: revision, ThroughStepId: through, Snapshot: string(snapshot), CreatedAt: time.Now()}
	if _, err := s.InsertOne(row); err != nil {
		return nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func Checkpoints(ownerID, id int64) ([]table.V22EditorCheckpoint, error) {
	rows := make([]table.V22EditorCheckpoint, 0)
	err := db.Instance().Where("owner_id = ? AND collector_id = ?", ownerID, id).Desc("created_at").Limit(100).Find(&rows)
	return rows, err
}
func DeleteCheckpoint(ownerID, id int64, key string) error {
	n, err := db.Instance().Where("id = ? AND owner_id = ? AND collector_id = ?", key, ownerID, id).Delete(new(table.V22EditorCheckpoint))
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
