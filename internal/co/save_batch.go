package co

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"sort"
	"strings"
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
	Port           string
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
	Port     string           `json:"port,omitempty"`
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
func PreviewSave(ctx context.Context, owner int64, version string, b SaveBinding, candidates []collector.Candidate, ports ...string) (any, error) {
	port := "records"
	if len(ports) > 0 && ports[0] != "" {
		port = ports[0]
	}
	if len(candidates) > 100 || len(collector.JSON(candidates)) > 512*1024 {
		return nil, collector.Invalid("candidates", "预演最多100条/512KiB")
	}
	s := session(ctx)
	defer s.Close()
	_, d, e := collectorVersion(s, owner, version)
	if e != nil {
		return nil, e
	}
	d, e = d.ForPort(port)
	if e != nil {
		return nil, e
	}
	if e = validateSaveMode(b); e != nil {
		return nil, e
	}
	if passThrough(b) {
		out := []SaveDecision{}
		for _, c := range candidates {
			if collector.CandidatePort(c) != port {
				return nil, collector.Invalid("candidates.port", "预演只能使用所选端口候选")
			}
			decision := SaveDecision{Port: port, Identity: c.Identity, Decision: "passed", Values: c.Values}
			if err := validateOutputCandidate(d, c); err != nil {
				decision.Decision = "invalid"
				decision.Error = collector.AsError(err)
			} else if !c.Valid {
				decision.Decision = "invalid"
			}
			out = append(out, decision)
		}
		return map[string]any{"schema_hash": collector.Hash(collector.PortSchema(d.Output)), "decisions": out, "formal_writes": 0}, nil
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
		if collector.CandidatePort(c) != port {
			return nil, collector.Invalid("candidates.port", "预演只能使用所选端口候选")
		}
		decision := SaveDecision{Port: port, Identity: c.Identity, Decision: "invalid", Values: c.Values}
		if problem := validateOutputCandidate(d, c); problem != nil {
			decision.Error = collector.AsError(problem)
		} else if c.Valid {
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
		decision.Port = port
		out = append(out, decision)
	}
	return map[string]any{"schema_hash": collector.Hash(schema), "decisions": out, "formal_writes": 0}, nil
}

// CommitBatch atomically saves business decisions, immutable outputs, O3
// dispatch plans and the page checkpoint under the run/stage/item lease fence.
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
	stage := c.Manifest.Stage(c.StageID)
	schemas, e := stageSchemas(s, r.OwnerID, stage, c.Manifest.CollectorDefinition)
	if e != nil {
		return e
	}
	// Lock business keys in canonical order, including keys that do not exist yet.
	keys := []string{}
	mapped := map[int]map[string]any{}
	keyByIndex := map[int]string{}
	invalid := map[int]error{}
	for i, candidate := range candidates {
		port := collector.CandidatePort(candidate)
		binding, err := stage.Binding(port)
		if err != nil {
			return err
		}
		cd, err := c.Manifest.CollectorDefinition.ForPort(port)
		if err != nil {
			return err
		}
		if err = validateOutputCandidate(cd, candidate); err != nil {
			invalid[i] = err
			continue
		}
		if !candidate.Valid {
			invalid[i] = collector.Fault("INVALID_CANDIDATE", "output", "候选字段验证未通过")
			continue
		}
		if passThrough(binding) {
			continue
		}
		schema := schemas[port]
		values, key, e := mapValues(binding, schema, candidate.Values)
		if e != nil {
			invalid[i] = e
			continue
		}
		mapped[i] = values
		keyByIndex[i] = key
		keys = append(keys, binding.TableID+":"+key)
	}
	sort.Strings(keys)
	previous := ""
	for _, key := range keys {
		if key == previous {
			continue
		}
		previous = key
		parts := strings.SplitN(key, ":", 2)
		if e = lockRecordKey(s, r.OwnerID, parts[0], parts[1]); e != nil {
			return e
		}
	}
	counts := map[string]int{}
	seenBusiness := map[string]string{}
	for i, candidate := range candidates {
		port := collector.CandidatePort(candidate)
		binding, _ := stage.Binding(port)
		schema := schemas[port]
		cd, _ := c.Manifest.CollectorDefinition.ForPort(port)
		schemaHash := binding.SchemaHash
		if passThrough(binding) {
			schemaHash = collector.Hash(collector.PortSchema(cd.Output))
		}
		identity := candidate.Identity
		if identity == "" {
			identity = "invalid:" + collector.Hash(candidate)
		}
		output := new(SavedOutput)
		exists, e := s.Where("item_id=? AND port=? AND output_identity=?", item.ID, port, identity).Get(output)
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
		} else if passThrough(binding) {
			decision.Decision = "passed"
		} else {
			values := mapped[i]
			key := keyByIndex[i]
			businessKey := binding.TableID + ":" + key
			if hash, ok := seenBusiness[businessKey]; ok && hash != collector.Hash(values) {
				decision.Decision = "conflict"
				decision.Error = collector.AsError(collector.Fault("SAVE_KEY_CONFLICT", "save", "同页不同候选映射到相同唯一键但值不同"))
			} else {
				seenBusiness[businessKey] = collector.Hash(values)
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
		if _, e = s.Exec(`INSERT INTO co_observations(id,run_id,item_id,record_id,output_identity,values_json,schema_hash,decision,diagnostic,port) VALUES(?,?,?,?,?,CAST(? AS jsonb),?,?,CAST(? AS jsonb),?)`, observation, r.ID, item.ID, recordID, identity, collector.JSON(candidate.Values), schemaHash, decision.Decision, collector.JSON(diagnostic), port); e != nil {
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
		out := SavedOutput{Port: port, ID: uuid.NewString(), RunID: r.ID, ItemID: item.ID, Namespace: r.Namespace, OutputIdentity: identity, Values: collector.JSON(candidate.Values), SchemaHash: schemaHash, ContentHash: collector.Hash(candidate.Values), ObservationID: observation, Decision: decision.Decision, Source: collector.JSON(map[string]any{"input_id": item.InputID, "source_key": item.SourceKey, "stage_id": c.StageID, "collector_version_id": c.Manifest.Collector.ID, "page": candidate.Page, "page_url": cp.CurrentURL, "content_hash": cp.ContentHash, "port": port, "attempt_id": c.Attempt.ID}), CreatedAt: time.Now()}
		if e = reserveStorage(s, r.OwnerID, "output_bytes", int64(len(out.Values)+len(out.Source))); e != nil {
			return e
		}
		if _, e = s.Insert(&out); e != nil {
			return e
		}
		if e = registerOutput(s, r, c, out); e != nil {
			return e
		}
		r.OutputCount++
	}
	var remaining int
	pendingRows, e := s.QueryString("SELECT COALESCE(SUM(total-next_ordinal),0) AS n FROM co_dispatch_plans WHERE run_id=? AND status='pending'", r.ID)
	if e != nil {
		return e
	}
	fmt.Sscan(pendingRows[0]["n"], &remaining)
	if remaining > dispatchLimit(c.Manifest) {
		return collector.Fault("DISPATCH_BUDGET", "budget", "当前页的分发量超过outbox容量；本页全部回滚，请缩小页批次或提高分发容量")
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
	if !cp.Done && remaining >= c.Manifest.Definition.Budget.MaxPending {
		if cp.Resume != "url_or_cursor" || len(c.Manifest.CollectorDefinition.Actions) > 0 || c.Manifest.CollectorDefinition.Fetch.Method == "POST" {
			return collector.Fault("BACKPRESSURE_UNSAFE", "dispatch", "当前页已保存；当前动作路径不能安全续页，请确认后重采或停止此项")
		}
		return collector.Fault("BACKPRESSURE", "dispatch", "当前页已保存，等待下游消化积压后从检查点继续")
	}
	return nil
}
