package co

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"sort"
	"time"
	"xorm.io/xorm"
)

type BusinessRecord struct {
	ID            string `xorm:"id pk"`
	TableID       string `xorm:"table_id"`
	CanonicalKey  string
	Values        string `xorm:"jsonb values_json"`
	Revision      int
	SchemaVersion int
	LastSeenAt    time.Time
}

func (BusinessRecord) TableName() string { return "co_records" }

type SavedOutput struct {
	ID             string `xorm:"id pk"`
	RunID          string `xorm:"run_id"`
	ItemID         string `xorm:"item_id"`
	Namespace      string
	OutputIdentity string
	Values         string `xorm:"jsonb values_json"`
	SchemaHash     string
	ContentHash    string
	ObservationID  string `xorm:"observation_id"`
	Decision       string
	Source         string `xorm:"jsonb source"`
	CreatedAt      time.Time
}

func (SavedOutput) TableName() string { return "co_run_outputs" }

type SaveDecision struct {
	Identity string           `json:"identity"`
	Decision string           `json:"decision"`
	RecordID string           `json:"record_id,omitempty"`
	Values   map[string]any   `json:"values"`
	Error    *collector.Error `json:"error,omitempty"`
}

func recordFor(s *xorm.Session, table, key string) (*BusinessRecord, error) {
	r := new(BusinessRecord)
	ok, e := s.Where("table_id=? AND canonical_key=?", table, key).Get(r)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, nil
	}
	return r, nil
}
func decide(old *BusinessRecord, values map[string]any, b SaveBinding) SaveDecision {
	decision := SaveDecision{Decision: "created", Values: values}
	if old != nil {
		previous := map[string]any{}
		_ = json.Unmarshal([]byte(old.Values), &previous)
		decision.Values = effectiveValues(previous, values, b)
		decision.RecordID = old.ID
		decision.Decision = "updated"
		if collector.Hash(previous) == collector.Hash(decision.Values) {
			decision.Decision = "unchanged"
		}
	}
	return decision
}
func PreviewSave(ctx context.Context, owner int64, version string, b SaveBinding, candidates []collector.Candidate) (any, error) {
	if len(candidates) > 100 || len(collector.JSON(candidates)) > 512*1024 {
		return nil, collector.Invalid("candidates", "预演最多100条/512KiB")
	}
	s := session(ctx)
	defer s.Close()
	_, d, e := collectorVersion(s, owner, version)
	if e != nil {
		return nil, e
	}
	schema, e := tableSchema(s, owner, b, false)
	if e != nil {
		return nil, e
	}
	if e = validateBinding(b, schema, d); e != nil {
		return nil, e
	}
	out := []SaveDecision{}
	seen := map[string]string{}
	for _, c := range candidates {
		decision := SaveDecision{Identity: c.Identity, Decision: "invalid", Values: c.Values}
		if c.Valid {
			values, key, e := mapValues(b, schema, c.Values)
			if e != nil {
				decision.Error = collector.AsError(e)
			} else {
				old, e := recordFor(s, b.TableID, key)
				if e != nil {
					return nil, e
				}
				decision = decide(old, values, b)
				decision.Identity = c.Identity
				if hash, ok := seen[key]; ok && hash != collector.Hash(values) {
					decision.Decision = "conflict"
				}
				seen[key] = collector.Hash(values)
			}
		}
		out = append(out, decision)
	}
	return map[string]any{"schema_hash": collector.Hash(schema), "decisions": out, "formal_writes": 0}, nil
}

// CommitBatch is the sole O2 business write boundary. O3 outbox insertion belongs
// inside this transaction, after immutable outputs and before the checkpoint.
func CommitBatch(ctx context.Context, c *WorkClaim, candidates []collector.Candidate, cp collector.Checkpoint) (err error) {
	defer func() {
		var typed *collector.Error
		if err != nil && !errors.As(err, &typed) {
			err = &collector.Error{Code: "BATCH_COMMIT_FAILED", Message: "本页保存未获确认；恢复时核对已保存检查点和输出身份", Retryable: true, Location: map[string]string{"step_id": "save"}}
		}
	}()
	if len(candidates) > 10000 || len(collector.JSON(candidates)) > 1024*1024 {
		return collector.Invalid("batch", "页批次超过上限")
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	r, item, e := lockClaim(s, c)
	if e != nil {
		return e
	}
	if time.Since(r.CreatedAt) > time.Duration(c.Manifest.Definition.Budget.MaxDurationMS)*time.Millisecond {
		return collector.Fault("DURATION_BUDGET", "run", "已达到运行总时长预算")
	}
	if cp.DefinitionHash != collector.Hash(c.Manifest.CollectorDefinition) || cp.InputHash != canonicalHash(item.InputValues) {
		return collector.Fault("CHECKPOINT_CONFLICT", "checkpoint", "检查点与冻结定义或输入不匹配")
	}
	binding := c.Manifest.Definition.Stages[0].Save
	schema, e := tableSchema(s, r.OwnerID, binding, true)
	if e != nil {
		return e
	}
	if e = validateBinding(binding, schema, c.Manifest.CollectorDefinition); e != nil {
		return e
	}
	// Lock business keys in canonical order, including keys that do not exist yet.
	keys := []string{}
	mapped := map[int]map[string]any{}
	keyByIndex := map[int]string{}
	invalid := map[int]error{}
	for i, candidate := range candidates {
		if !candidate.Valid {
			invalid[i] = collector.Fault("INVALID_CANDIDATE", "output", "候选字段验证未通过")
			continue
		}
		values, key, e := mapValues(binding, schema, candidate.Values)
		if e != nil {
			invalid[i] = e
			continue
		}
		mapped[i] = values
		keyByIndex[i] = key
		keys = append(keys, key)
	}
	sort.Strings(keys)
	previous := ""
	for _, key := range keys {
		if key == previous {
			continue
		}
		previous = key
		if e = lockRecordKey(s, r.OwnerID, binding.TableID, key); e != nil {
			return e
		}
	}
	counts := map[string]int{}
	seenBusiness := map[string]string{}
	for i, candidate := range candidates {
		identity := candidate.Identity
		if identity == "" {
			identity = "invalid:" + collector.Hash(candidate)
		}
		output := new(SavedOutput)
		exists, e := s.Where("item_id=? AND output_identity=?", item.ID, identity).Get(output)
		if e != nil {
			return e
		}
		if exists {
			if output.ContentHash != collector.Hash(candidate.Values) {
				return collector.Fault("OUTPUT_CHANGED_ON_REPLAY", "output", "同一工作项输出身份的内容发生变化，原输出快照保留；请检查来源后重新运行")
			}
			counts["replayed"]++
			continue
		}
		decision := SaveDecision{Identity: identity, Decision: "invalid", Values: candidate.Values}
		var recordID any
		var record *BusinessRecord
		if problem, ok := invalid[i]; ok {
			decision.Error = collector.AsError(problem)
		} else {
			values := mapped[i]
			key := keyByIndex[i]
			if hash, ok := seenBusiness[key]; ok && hash != collector.Hash(values) {
				decision.Decision = "conflict"
				decision.Error = collector.AsError(collector.Fault("SAVE_KEY_CONFLICT", "save", "同页不同候选映射到相同唯一键但值不同"))
			} else {
				seenBusiness[key] = collector.Hash(values)
				record, e = recordFor(s, binding.TableID, key)
				if e != nil {
					return e
				}
				decision = decide(record, values, binding)
				decision.Identity = identity
				// Required constraints apply to the effective row as well as incoming values.
				for _, f := range schema.Fields {
					value, present := decision.Values[f.Key]
					if !present && f.Required {
						decision.Error = collector.AsError(collector.Invalid("save."+f.Key, "更新策略将移除必填字段"))
						break
					}
					if present {
						if _, err := collector.ValidateValue(value, f, false); err != nil {
							decision.Error = collector.AsError(collector.Invalid("save."+f.Key, err.Error()))
							break
						}
					}
				}
				if decision.Error != nil {
					decision.Decision = "invalid"
				} else if r.Namespace == "formal" {
					if record == nil {
						record = &BusinessRecord{ID: uuid.NewString(), TableID: binding.TableID, CanonicalKey: key, Values: collector.JSON(decision.Values), Revision: 1, SchemaVersion: binding.SchemaVersion, LastSeenAt: time.Now()}
						if _, e = s.Insert(record); e != nil {
							return e
						}
					} else {
						record.LastSeenAt = time.Now()
						if decision.Decision == "updated" {
							record.Revision++
							record.Values = collector.JSON(decision.Values)
							record.SchemaVersion = binding.SchemaVersion
						}
						if _, e = s.ID(record.ID).Cols("revision", "values_json", "schema_version", "last_seen_at").Update(record); e != nil {
							return e
						}
					}
					recordID = record.ID
					decision.RecordID = record.ID
				}
			}
		}
		observation := uuid.NewString()
		diagnostic := map[string]any{"fields": candidate.Fields, "page_url": cp.CurrentURL, "content_hash": cp.ContentHash}
		if decision.Error != nil {
			diagnostic["error"] = decision.Error
		}
		if _, e = s.Exec(`INSERT INTO co_observations(id,run_id,item_id,record_id,output_identity,values_json,schema_hash,decision,diagnostic) VALUES(?,?,?,?,?,CAST(? AS jsonb),?,?,CAST(? AS jsonb))`, observation, r.ID, item.ID, recordID, identity, collector.JSON(candidate.Values), binding.SchemaHash, decision.Decision, collector.JSON(diagnostic)); e != nil {
			return e
		}
		counts[decision.Decision]++
		if decision.Decision == "invalid" || decision.Decision == "conflict" {
			continue
		}
		if recordID != nil && (decision.Decision == "created" || decision.Decision == "updated") {
			if _, e = s.Exec(`INSERT INTO co_record_revisions(record_id,revision,observation_id,values_json) VALUES(?,?,?,CAST(? AS jsonb))`, record.ID, record.Revision, observation, record.Values); e != nil {
				return e
			}
		}
		if r.OutputCount >= c.Manifest.Definition.Budget.MaxOutputs {
			return collector.Fault("OUTPUT_BUDGET", "budget", "达到输出预算；本页未提交，之前保存的数据保留")
		}
		out := SavedOutput{ID: uuid.NewString(), RunID: r.ID, ItemID: item.ID, Namespace: r.Namespace, OutputIdentity: identity, Values: collector.JSON(candidate.Values), SchemaHash: binding.SchemaHash, ContentHash: collector.Hash(candidate.Values), ObservationID: observation, Decision: decision.Decision, Source: collector.JSON(map[string]any{"input_id": item.InputID, "source_key": item.SourceKey, "stage_id": c.Manifest.Definition.EntryStage, "collector_version_id": c.Manifest.Collector.ID, "page": candidate.Page, "page_url": cp.CurrentURL, "content_hash": cp.ContentHash, "port": "records", "attempt_id": c.Attempt.ID}), CreatedAt: time.Now()}
		if _, e = s.Insert(&out); e != nil {
			return e
		}
		r.OutputCount++
	}
	if _, e = s.Exec("UPDATE co_runs SET output_count=? WHERE id=?", r.OutputCount, r.ID); e != nil {
		return e
	}
	if cp.Done && cp.FinalStatus == "succeeded" {
		n, e := s.Table("co_observations").Where("item_id=? AND decision IN ('invalid','conflict')", item.ID).Count()
		if e != nil {
			return e
		}
		if n > 0 {
			cp.FinalStatus = "partial"
			cp.FinalReason = "INVALID_OR_CONFLICTING_CANDIDATES"
		}
	}
	if _, e = s.Exec("UPDATE co_work_items SET checkpoint=CAST(? AS jsonb) WHERE id=?", collector.JSON(cp), item.ID); e != nil {
		return e
	}
	if e = event(s, r, "batch_committed", item.ID, map[string]any{"page": cp.Pages, "records": cp.Records, "done": cp.Done, "decisions": counts, "namespace": r.Namespace}); e != nil {
		return e
	}
	paused := r.Status == "pausing"
	if paused {
		if _, e = s.Exec("UPDATE co_work_items SET status='queued',lease_token=NULL,lease_until=NULL WHERE id=?", item.ID); e != nil {
			return e
		}
		if _, e = s.Exec("UPDATE co_work_attempts SET status='paused',finished_at=NOW() WHERE id=?", c.Attempt.ID); e != nil {
			return e
		}
		if e = settle(s, r); e != nil {
			return e
		}
	}
	if e = s.Commit(); e != nil {
		return e
	}
	if paused {
		return collector.Fault("CONTROL_PAUSED", "run", "已在本页保存后暂停，恢复将使用已提交检查点")
	}
	return nil
}
