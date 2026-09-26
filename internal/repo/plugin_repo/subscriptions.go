package plugin_repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"xorm.io/xorm"
)

type SubscriptionInput struct {
	ID         int64  `json:"id"`
	DatasetID  int64  `json:"dataset_id"`
	WorkflowID *int64 `json:"workflow_id"`
	PluginCode string `json:"plugin_code"`
	EventType  string `json:"event_type"`
	URLField   string `json:"url_field"`
	Enabled    bool   `json:"enabled"`
}

// Subscriptions returns dataset-wide subscriptions plus this collector's subscriptions.
func Subscriptions(datasetID, workflowID int64) ([]table.PluginSubscription, error) {
	rows := make([]table.PluginSubscription, 0)
	if db.Instance() == nil {
		return rows, errors.New("database is not initialized")
	}
	if datasetID <= 0 {
		return rows, errors.New("dataset id is required")
	}
	s := db.Instance().Where("dataset_id=?", datasetID)
	if workflowID > 0 {
		s.And("(workflow_id IS NULL OR workflow_id=?)", workflowID)
	}
	err := s.Asc("id").Find(&rows)
	return rows, err
}

func SaveSubscription(in SubscriptionInput) (*table.PluginSubscription, error) {
	if db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	if in.DatasetID <= 0 || strings.TrimSpace(in.PluginCode) == "" || strings.TrimSpace(in.URLField) == "" || (in.EventType != "record.created" && in.EventType != "record.updated") || (in.WorkflowID != nil && *in.WorkflowID <= 0) {
		return nil, errors.New("invalid subscription scope, plugin or event")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	var dataset table.Dataset
	has, err := s.SQL("SELECT * FROM datasets WHERE id=? FOR UPDATE", in.DatasetID).Get(&dataset)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, errors.New("dataset not found")
	}
	if in.WorkflowID != nil {
		var owner table.Workflow
		has, err = s.ID(*in.WorkflowID).Get(&owner)
		if err != nil {
			return nil, err
		}
		if !has || owner.DatasetId == nil || *owner.DatasetId != in.DatasetID {
			return nil, errors.New("collector does not belong to dataset")
		}
	}
	var field table.DatasetField
	has, err = s.Where("dataset_id=? AND schema_version=? AND field_key=? AND multiple=false AND field_type IN ('url','string')", in.DatasetID, dataset.SchemaVersion, in.URLField).Get(&field)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, errors.New("URL field must be a scalar url or string field in the current dataset schema")
	}
	row := &table.PluginSubscription{Id: in.ID, DatasetId: in.DatasetID, WorkflowId: in.WorkflowID, PluginCode: strings.TrimSpace(in.PluginCode), EventType: in.EventType, URLField: in.URLField, Enabled: in.Enabled, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if in.ID > 0 {
		var old table.PluginSubscription
		has, err := s.ID(in.ID).Get(&old)
		if err != nil {
			return nil, err
		}
		if !has || old.DatasetId != in.DatasetID || old.PluginCode != row.PluginCode || old.EventType != row.EventType || (old.WorkflowId == nil) != (row.WorkflowId == nil) || (old.WorkflowId != nil && *old.WorkflowId != *row.WorkflowId) {
			return nil, errors.New("subscription identity cannot be changed")
		}
		affected, err := s.ID(in.ID).Where("dataset_id=?", in.DatasetID).Cols("workflow_id", "plugin_code", "event_type", "url_field", "enabled", "updated_at").Update(row)
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			return nil, errors.New("subscription not found in dataset")
		}
	} else {
		if _, err := s.InsertOne(row); err != nil {
			return nil, err
		}
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func DeleteSubscription(datasetID, id int64) error {
	if db.Instance() == nil {
		return errors.New("database is not initialized")
	}
	if datasetID <= 0 || id <= 0 {
		return errors.New("dataset and subscription ids are required")
	}
	n, err := db.Instance().Where("dataset_id=? AND id=?", datasetID, id).Delete(new(table.PluginSubscription))
	if err == nil && n == 0 {
		return errors.New("subscription not found in dataset")
	}
	return err
}

// QueueRecordEvent shares the record transaction. No external calls occur here.
// The plugin worker can only observe these tasks after the caller commits.
func QueueRecordEvent(s *xorm.Session, datasetID, workflowID, recordID, observationID int64, event string, values map[string]any, provenance map[string]any) error {
	var rows []table.PluginSubscription
	if err := s.Where("dataset_id=? AND enabled=true AND event_type=? AND (workflow_id IS NULL OR workflow_id=?)", datasetID, event, workflowID).Asc("id").Find(&rows); err != nil {
		return err
	}
	for _, sub := range rows {
		// Freeze the URL mapping and record snapshot; retry never reads a later record version.
		input := map[string]any{"url": values[sub.URLField], "record": values, "provenance": provenance, "subscription": sub}
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if len(encoded) > 1024*1024 {
			return errors.New("plugin event exceeds size limit")
		}
		key := fmt.Sprintf("subscription:%d:observation:%d", sub.Id, observationID)
		_, err = s.Exec(`INSERT INTO plugin_tasks(record_id,observation_id,dataset_id,workflow_id,subscription_id,event_type,plugin_code,idempotency_key,status,input)
   VALUES(?,?,?,?,?,?,?,?,'queued',?::jsonb) ON CONFLICT(plugin_code,idempotency_key) DO NOTHING`, recordID, observationID, datasetID, workflowID, sub.Id, event, sub.PluginCode, key, string(encoded))
		if err != nil {
			return err
		}
	}
	return nil
}
