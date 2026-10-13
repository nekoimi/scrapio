package co

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"io"
	"strings"
	"time"
	"unicode/utf8"
	"xorm.io/xorm"
)

// Sources are finite, confirmed assets, never pointers to mutable latest records.
const sourceRowsMax = 1000

type SourceRequest struct {
	Kind               string            `json:"kind"`
	CollectorVersionID string            `json:"collector_version_id"`
	TableID            string            `json:"table_id,omitempty"`
	SchemaVersion      int               `json:"schema_version,omitempty"`
	RecordIDs          []string          `json:"record_ids,omitempty"`
	Filter             map[string]any    `json:"filter,omitempty"`
	Limit              int               `json:"limit,omitempty"`
	Format             string            `json:"format,omitempty"`
	Content            string            `json:"content,omitempty"`
	Mapping            map[string]string `json:"mapping"`
}
type SourceRow struct {
	Row         int              `json:"row"`
	Values      map[string]any   `json:"values,omitempty"`
	Source      map[string]any   `json:"source"`
	Error       *collector.Error `json:"error,omitempty"`
	DuplicateOf int              `json:"duplicate_of,omitempty"`
}
type SourceSnapshot struct {
	ID                 string `xorm:"id pk"`
	OwnerID            int64  `xorm:"owner_id"`
	Kind               string
	CollectorVersionID string `xorm:"collector_version_id"`
	ContentHash        string
	RowCount           int
	ValidCount         int
	RowsJSON           string `xorm:"jsonb rows_json"`
	Provenance         string `xorm:"jsonb provenance"`
	OriginalContent    string
	CreatedAt          time.Time
	Expired            bool
	AccountedBytes     int64
}

func (SourceSnapshot) TableName() string { return "co_source_snapshots" }
func sourceCount(s ManualSource) int {
	if s.Kind == "manual" {
		return len(s.Inputs)
	}
	return s.Count
}
func validateTaskSource(s ManualSource) error {
	if s.Kind == "manual" {
		if s.SnapshotID != "" || len(s.Inputs) < 1 || len(s.Inputs) > 100 || len(collector.JSON(s.Inputs)) > 512*1024 {
			return collector.Invalid("source", "手动输入需要1..100条/512KiB，不可混用快照")
		}
		return nil
	}
	if (s.Kind != "table" && s.Kind != "file") || len(s.Inputs) > 0 || s.SnapshotID == "" || s.SnapshotHash == "" || s.Count < 1 || s.Count > sourceRowsMax {
		return collector.Invalid("source", "表或文件输入需要已确认的快照ID、指纹和1..1000条有效输入")
	}
	return nil
}
func sourceAsset(s *xorm.Session, owner int64, src ManualSource) (*SourceSnapshot, error) {
	if _, e := s.Exec("SELECT pg_advisory_xact_lock(hashtextextended('co-source-retention',0))"); e != nil {
		return nil, e
	}
	a := new(SourceSnapshot)
	ok, e := s.Where("owner_id=? AND id=?", owner, src.SnapshotID).Get(a)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, collector.Fault("SOURCE_EVIDENCE_MISSING", "source", "输入快照不存在，不能改用表当前值")
	}
	if a.Expired {
		return nil, collector.Fault("SOURCE_EVIDENCE_EXPIRED", "source", "未使用的输入证据已过期；请显式重新取样并确认")
	}
	if a.Kind != src.Kind || a.ContentHash != src.SnapshotHash || a.ValidCount != src.Count {
		return nil, collector.Invalid("source", "快照类型、指纹或条数不匹配")
	}
	if a.ValidCount != a.RowCount && !src.AcceptPartial {
		return nil, collector.Invalid("source.accept_partial", "含无效或重复行，请确认仅导入有效且不重复的行")
	}
	return a, nil
}
func frozenSourceInputs(s *xorm.Session, owner int64, src ManualSource, d collector.Definition) ([]RunInput, error) {
	out := []RunInput{}
	if src.Kind == "manual" {
		rows, e := PreviewInputs(d, src.Inputs)
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			if r.Error != nil {
				return nil, r.Error
			}
			out = append(out, RunInput{SourceKey: r.SourceKey, Values: collector.JSON(r.Values), Source: collector.JSON(map[string]any{"kind": "manual", "ordinal": r.Ordinal})})
		}
		return out, nil
	}
	a, e := sourceAsset(s, owner, src)
	if e != nil {
		return nil, e
	}
	var rows []SourceRow
	if e = collector.Strict([]byte(a.RowsJSON), &rows); e != nil {
		return nil, e
	}
	for _, row := range rows {
		if row.Error != nil || row.DuplicateOf > 0 {
			continue
		}
		v, e := collector.ValidateInput(d, row.Values)
		if e != nil {
			return nil, e
		}
		if _, e = collector.Prepare(d, v); e != nil {
			return nil, e
		}
		provenance := map[string]any{"kind": a.Kind, "snapshot_id": a.ID, "snapshot_hash": a.ContentHash, "row": row.Row, "original": row.Source}
		out = append(out, RunInput{SourceKey: fmt.Sprintf("snapshot:%s:%d", a.ID, row.Row), Values: collector.JSON(v), Source: collector.JSON(provenance)})
	}
	if len(out) != src.Count {
		return nil, collector.Invalid("source.count", "快照有效条数不一致")
	}
	return out, nil
}
func parseSourceFile(format, content string) ([]map[string]any, error) {
	if len(content) == 0 || len(content) > 768*1024 || !utf8.ValidString(content) {
		return nil, collector.Invalid("content", "文件需要1..768KiB UTF-8文本")
	}
	rows := []map[string]any{}
	switch format {
	case "json":
		if e := collector.Strict([]byte(content), &rows); e != nil {
			return nil, e
		}
	case "csv":
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\uFEFF")))
		header, e := r.Read()
		if e != nil {
			return nil, collector.Invalid("content", "CSV缺少表头")
		}
		seen := map[string]bool{}
		for _, k := range header {
			if strings.TrimSpace(k) == "" || seen[k] {
				return nil, collector.Invalid("content", "CSV表头不能为空或重复")
			}
			seen[k] = true
		}
		for {
			v, e := r.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, collector.Invalid("content", fmt.Sprintf("CSV第%d行格式无效", len(rows)+2))
			}
			row := map[string]any{}
			for i, k := range header {
				row[k] = v[i]
			}
			rows = append(rows, row)
			if len(rows) > sourceRowsMax {
				break
			}
		}
	default:
		return nil, collector.Invalid("format", "首版支持CSV（含表头）和JSON对象数组")
	}
	if len(rows) < 1 || len(rows) > sourceRowsMax {
		return nil, collector.Invalid("content", "文件需要1..1000行")
	}
	for _, row := range rows {
		if row == nil {
			return nil, collector.Invalid("content", "JSON每行必须是对象")
		}
	}
	return rows, nil
}
func SourcePreview(ctx context.Context, owner int64, key string, p SourceRequest) (*Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if (p.Kind != "table" && p.Kind != "file") || len(p.Mapping) == 0 || len(p.Mapping) > 30 {
		return nil, collector.Invalid("source", "请选择表/文件来源及输入列映射")
	}
	return mutate(ctx, owner, "snapshotSource", "", key, p, false, true, func(s *xorm.Session, o *Operation) (any, error) {
		o.Payload = collector.JSON(map[string]any{"kind": p.Kind, "request_hash": collector.Hash(p)})
		_, d, e := collectorVersion(s, owner, p.CollectorVersionID)
		if e != nil {
			return nil, e
		}
		fields := map[string]collector.Field{}
		for _, f := range d.InputFields {
			fields[f.Key] = f
		}
		for target := range p.Mapping {
			if _, ok := fields[target]; !ok {
				return nil, collector.Invalid("mapping."+target, "采集器没有这个输入字段")
			}
		}
		raw := []map[string]any{}
		origins := []map[string]any{}
		if p.Kind == "file" {
			raw, e = parseSourceFile(p.Format, p.Content)
			if e != nil {
				return nil, e
			}
			for i := range raw {
				origins = append(origins, map[string]any{"file_row": i + 1, "format": p.Format, "content_hash": collector.Hash(p.Content)})
			}
		} else {
			if len(p.RecordIDs) > sourceRowsMax || p.Limit < 1 || p.Limit > sourceRowsMax {
				return nil, collector.Invalid("limit", "表输入上限1..1000行")
			}
			t := new(DataTable)
			ok, e := s.Where("owner_id=? AND id=?", owner, p.TableID).Get(t)
			if e != nil {
				return nil, e
			}
			if !ok {
				return nil, ErrNotFound
			}
			if t.CurrentSchema != p.SchemaVersion {
				return nil, collector.Fault("SCHEMA_CONFLICT", "source", "来源表Schema已变化，请重新选择")
			}
			q := "SELECT * FROM co_records WHERE table_id=? AND schema_version<=?"
			args := []any{p.TableID, p.SchemaVersion}
			if len(p.RecordIDs) > 0 {
				b, _ := json.Marshal(p.RecordIDs)
				q += " AND id IN (SELECT jsonb_array_elements_text(CAST(? AS jsonb)))"
				args = append(args, string(b))
			}
			if len(p.Filter) > 0 {
				q += " AND values_json @> CAST(? AS jsonb)"
				args = append(args, collector.JSON(p.Filter))
			}
			q += " ORDER BY id LIMIT ?"
			args = append(args, p.Limit+1)
			records := []BusinessRecord{}
			if e = s.SQL(q, args...).Find(&records); e != nil {
				return nil, e
			}
			if len(records) > p.Limit {
				return nil, collector.Invalid("limit", "查询结果超过边界；请缩小条件或明确增加上限，不静默截断")
			}
			if len(p.RecordIDs) > 0 && len(records) != len(p.RecordIDs) {
				return nil, collector.Invalid("record_ids", "选中记录缺失、重复或不符合Schema/条件，请重新选择")
			}
			for _, record := range records {
				var v map[string]any
				if e = collector.Strict([]byte(record.Values), &v); e != nil {
					return nil, e
				}
				raw = append(raw, v)
				origins = append(origins, map[string]any{"table_id": p.TableID, "record_id": record.ID, "revision": record.Revision, "schema_version": record.SchemaVersion, "values": v})
			}
		}
		if len(raw) == 0 {
			return nil, collector.Invalid("source", "没有符合条件的输入")
		}
		rows := []SourceRow{}
		seen := map[string]int{}
		valid := 0
		for i, row := range raw {
			values := map[string]any{}
			for target, column := range p.Mapping {
				if v, ok := row[column]; ok {
					if p.Kind == "file" && p.Format == "csv" && fields[target].Type != "string" {
						var converted any
						if json.Unmarshal([]byte(fmt.Sprint(v)), &converted) == nil {
							v = converted
						}
					}
					values[target] = v
				}
			}
			v, e := collector.ValidateInput(d, values)
			if e == nil {
				_, e = collector.Prepare(d, v)
			}
			r := SourceRow{Row: i + 1, Source: origins[i], Values: v}
			if e != nil {
				r.Error = collector.AsError(e)
			} else {
				h := collector.Hash(v)
				if original, ok := seen[h]; ok {
					r.DuplicateOf = original
				} else {
					valid++
					seen[h] = i + 1
				}
			}
			rows = append(rows, r)
		}
		encoded := collector.JSON(rows)
		if len(encoded) > 4*1024*1024 {
			return nil, collector.Invalid("source", "来源及映射快照超过4MiB，请缩小批次")
		}
		hash := collector.Hash(map[string]any{"request": p, "rows": rows})
		a := SourceSnapshot{ID: uuid.NewString(), OwnerID: owner, Kind: p.Kind, CollectorVersionID: p.CollectorVersionID, ContentHash: hash, RowCount: len(rows), ValidCount: valid, RowsJSON: encoded, Provenance: collector.JSON(p), OriginalContent: p.Content, CreatedAt: time.Now()}
		a.AccountedBytes = int64(len(encoded) + len(a.Provenance) + len(p.Content))
		if e = reserveStorage(s, owner, "source_bytes", a.AccountedBytes); e != nil {
			return nil, e
		}
		if _, e = s.Insert(&a); e != nil {
			return nil, e
		}
		// The operation response is bounded, while the asset retains every row and file.
		preview := rows
		if len(preview) > 100 {
			preview = preview[:100]
		}
		return map[string]any{"snapshot_id": a.ID, "snapshot_hash": hash, "kind": p.Kind, "row_count": len(rows), "valid_count": valid, "partial": valid != len(rows), "items": preview, "preview_limit": 100}, nil
	})
}
func SourceDetail(ctx context.Context, owner int64, id string, offset, limit int) (any, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, collector.Invalid("page", "快照页大小1..100")
	}
	s := session(ctx)
	defer s.Close()
	a := new(SourceSnapshot)
	ok, e := s.Where("owner_id=? AND id=?", owner, id).Get(a)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	var rows []SourceRow
	if !a.Expired {
		if e = collector.Strict([]byte(a.RowsJSON), &rows); e != nil {
			return nil, e
		}
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	return map[string]any{"id": a.ID, "kind": a.Kind, "snapshot_hash": a.ContentHash, "row_count": a.RowCount, "valid_count": a.ValidCount, "expired": a.Expired, "provenance": json.RawMessage(a.Provenance), "items": rows[offset:end], "next_offset": end, "created_at": a.CreatedAt}, nil
}
