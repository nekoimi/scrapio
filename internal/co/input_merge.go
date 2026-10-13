package co

import (
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

// Empty policy preserves old manifests. A stage is the semantic boundary:
// one frozen collector, save binding, delivery policy and execution namespace.
func mergeKey(stage TaskStage, raw string) string {
	if stage.InputPolicy != "merge_identical" {
		return ""
	}
	return collector.Hash([]string{stage.CollectorVersionID, canonicalHash(raw)})
}

// All callers hold the run lock. The partial unique index also fences a
// programming error which tries to create a second canonical item.
func mergedItem(s *xorm.Session, runID, stageRunID string, stage TaskStage, raw string) (string, error) {
	key := mergeKey(stage, raw)
	if key == "" {
		return "", nil
	}
	rows, e := s.QueryString("SELECT id FROM co_work_items WHERE run_id=? AND stage_run_id=? AND dedup_key=?", runID, stageRunID, key)
	if e != nil || len(rows) == 0 {
		return "", e
	}
	return rows[0]["id"], nil
}

func createInputItem(s *xorm.Session, runID string, stageRunID string, stage TaskStage, definition collector.Definition, in RunInput) (string, error) {
	key := mergeKey(stage, in.Values)
	var storedKey any
	if key != "" {
		storedKey = key
	}
	id := uuid.NewString()
	_, e := s.Exec(`INSERT INTO co_work_items(id,run_id,stage_run_id,input_id,source_key,input_values,input_hash,site_key,dedup_key) VALUES(?,?,?,?,?,CAST(? AS jsonb),?,?,?)`, id, runID, stageRunID, in.ID, in.SourceKey, in.Values, canonicalHash(in.Values), executionSiteInput(definition, in.Values), storedKey)
	return id, e
}

func attachInputSource(s *xorm.Session, runID, itemID, inputID string, merged bool) error {
	_, e := s.Exec(`INSERT INTO co_item_sources(run_id,item_id,input_id,merged) VALUES(?,?,?,?)`, runID, itemID, inputID, merged)
	return e
}
