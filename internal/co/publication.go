package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/collector"
	"time"
	"xorm.io/xorm"
)

func SaveSample(ctx context.Context, owner int64, id, key string, revision int, evidence string, expected []map[string]any) (*Operation, error) {
	if len(expected) > 100 || len(collector.JSON(expected)) > 1024*1024 {
		return nil, collector.Invalid("expected_outputs", "预期最多100条、1MiB")
	}
	return Mutate(ctx, owner, "saveSample", id, key, map[string]any{"revision": revision, "evidence_id": evidence, "expected_outputs": expected}, false, func(s *xorm.Session, o *Operation) (any, error) {
		c, e := getCollector(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if c.Revision != revision || c.Archived {
			return nil, conflict("草稿已变化或归档")
		}
		op := new(Operation)
		ok, e := s.Where("owner_id=? AND resource_id=? AND id=? AND operation='testCollector' AND status='succeeded'", owner, id, evidence).Get(op)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		var p TestPayload
		var result collector.Result
		var raw struct {
			Snapshots []collector.Document `json:"snapshots"`
		}
		if json.Unmarshal([]byte(op.Payload), &p) != nil || json.Unmarshal([]byte(op.Result), &result) != nil || json.Unmarshal([]byte(op.Result), &raw) != nil || p.Revision != revision || result.DefinitionHash != collector.Hash(p.Definition) {
			return nil, conflict("证据不属于当前草稿")
		}
		if len(raw.Snapshots) == 0 {
			return nil, collector.Invalid("evidence_id", "证据没有可保存的有限快照（最多3页，每页256KiB）")
		}
		count, e := s.Where("owner_id=? AND collector_id=?", owner, id).Count(new(Sample))
		if e != nil {
			return nil, e
		}
		if count >= 20 {
			return nil, conflict("每采集器最多20个样例，请清理旧样例")
		}
		if expected == nil {
			expected = []map[string]any{}
			for _, c := range result.Candidates {
				if c.Valid {
					expected = append(expected, c.Values)
				}
			}
		}
		row := Sample{ID: uuid.NewString(), OwnerID: owner, CollectorID: id, Revision: revision, Input: collector.JSON(result.Input), Snapshots: collector.JSON(raw.Snapshots), ExpectedOutputs: collector.JSON(expected), CreatedAt: time.Now()}
		if _, e = s.Insert(&row); e != nil {
			return nil, e
		}
		return row.View(), nil
	})
}
func CheckEvidence(op *Operation, p TestPayload) (map[string]any, error) {
	if op.Operation != "testCollector" || op.ResourceID == "" || op.Status != "succeeded" {
		return nil, collector.Invalid("evidence_id", "请选择已结束的采集器测试")
	}
	var frozen TestPayload
	var result collector.Result
	if json.Unmarshal([]byte(op.Payload), &frozen) != nil || json.Unmarshal([]byte(op.Result), &result) != nil {
		return nil, collector.Invalid("evidence_id", "测试证据损坏")
	}
	if frozen.Revision != p.Revision || result.DefinitionHash != collector.Hash(p.Definition) {
		return nil, conflict("测试证据与当前草稿revision/定义不一致")
	}
	if result.Mode != "live" {
		return nil, collector.Invalid("evidence_id", "离线回归可校验规则，但发布需要当前草稿的实时获取证据")
	}
	if result.Status != "succeeded" && result.Status != "limited" {
		return nil, collector.Invalid("evidence_id", "存在无效候选、获取失败或未知动作，不能发布")
	}
	if len(result.Pages) == 0 {
		return nil, collector.Invalid("evidence_id", "缺少页面获取证据")
	}
	for _, c := range result.Candidates {
		if !c.Valid {
			return nil, collector.Invalid("evidence_id", "候选包含无效字段，请先修正规则")
		}
	}
	if len(result.Candidates) == 0 && !result.Pages[len(result.Pages)-1].EmptyVerified {
		return nil, collector.Invalid("evidence_id", "零输出缺少明确结束证据")
	}
	if result.Status == "limited" && result.Reason != "PAGINATION_BUDGET" && result.Reason != "RECORD_OR_EVIDENCE_BUDGET" {
		return nil, collector.Invalid("evidence_id", "测试覆盖不足，请补充实时测试")
	}
	warnings := []string{}
	if result.Status == "limited" {
		warnings = append(warnings, "发布只证明测试范围，未证明全量分页；任务运行仍遵循冻结预算")
	}
	return map[string]any{"allowed": true, "revision": p.Revision, "definition_hash": collector.Hash(p.Definition), "evidence_id": op.ID, "warnings": warnings}, nil
}
func Publish(ctx context.Context, owner int64, id, key string, revision int, checkID string) (*Operation, error) {
	return Mutate(ctx, owner, "publishCollector", id, key, map[string]any{"revision": revision, "check_id": checkID}, false, func(s *xorm.Session, o *Operation) (any, error) {
		c, e := getCollector(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if c.Archived || c.Revision != revision {
			return nil, conflict("草稿已变化或归档，请重新检查")
		}
		d, e := collector.Decode([]byte(c.Definition))
		if e != nil {
			return nil, e
		}
		check := new(Operation)
		ok, e := s.Where("owner_id=? AND resource_id=? AND id=? AND operation='checkPublication' AND status='succeeded'", owner, id, checkID).Get(check)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		var result struct {
			Allowed        bool   `json:"allowed"`
			Revision       int    `json:"revision"`
			DefinitionHash string `json:"definition_hash"`
			EvidenceID     string `json:"evidence_id"`
		}
		if json.Unmarshal([]byte(check.Result), &result) != nil || !result.Allowed || result.Revision != revision || result.DefinitionHash != collector.Hash(d) || time.Since(check.CreatedAt) > 10*time.Minute {
			return nil, conflict("发布检查已过期，请重新检查")
		}
		version := new(Version)
		if found, e := s.Where("collector_id=? AND revision=?", id, revision).Get(version); e != nil {
			return nil, e
		} else if found {
			return version.View(), nil
		}
		count, e := s.Where("collector_id=?", id).Count(new(Version))
		if e != nil {
			return nil, e
		}
		v := Version{ID: uuid.NewString(), OwnerID: owner, CollectorID: id, Number: int(count) + 1, Revision: revision, Definition: c.Definition, DefinitionHash: collector.Hash(d), InputSchemaHash: collector.Hash(d.InputFields), OutputSchemaHash: collector.OutputSchemaHash(d), InterpreterVersion: collector.Interpreter, PublishVersion: "co.publish.v1", Requirements: collector.JSON(map[string]any{"fetch": d.Fetch.Kind, "browser_protocol": browserRequirement(d), "origins": d.Fetch.AllowedOrigins, "credential_ref": d.Fetch.CredentialRef}), EvidenceID: result.EvidenceID, CheckID: checkID, CreatedAt: time.Now()}
		_, e = s.Insert(&v)
		return v.View(), e
	})
}
func browserRequirement(d collector.Definition) string {
	if d.Fetch.Kind == "browser" {
		return "editor.isolated.v1"
	}
	return ""
}
func Compare(actual []collector.Candidate, expected []map[string]any) map[string]any {
	valid := []map[string]any{}
	for _, c := range actual {
		if c.Valid {
			valid = append(valid, c.Values)
		}
	}
	equal := collector.Hash(valid) == collector.Hash(expected)
	return map[string]any{"status": map[bool]string{true: "matched", false: "different"}[equal], "actual_count": len(valid), "expected_count": len(expected), "actual_hash": collector.Hash(valid), "expected_hash": collector.Hash(expected)}
}
func ValidateDocuments(docs []collector.Document) error {
	if len(docs) > 3 {
		return collector.Invalid("snapshots", "最多3页")
	}
	for _, d := range docs {
		if len(d.Content) > 256*1024 || d.Format != "html" && d.Format != "json" {
			return collector.Invalid("snapshots", "快照格式或大小无效")
		}
		if _, e := capture.ValidateURL(d.URL); e != nil {
			return fmt.Errorf("invalid snapshot URL")
		}
	}
	return nil
}
