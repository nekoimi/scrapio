package co

import (
	"context"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/collector"
	"time"
	"xorm.io/xorm"
)

func Tests(ctx context.Context, owner int64, id, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows := []Operation{}
	q := s.Where("owner_id=? AND resource_id=? AND operation='testCollector'", owner, id)
	if cursor != "" {
		boundary, e := decodeCursor(cursor)
		if e != nil {
			return nil, e
		}
		q = q.And("(created_at,id)<(?,?)", boundary.At, boundary.ID)
	}
	e := q.OrderBy("created_at DESC,id DESC").Limit(limit + 1).Find(&rows)
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	items := []any{}
	for _, r := range rows {
		items = append(items, r.View())
	}
	return map[string]any{"items": items, "next_cursor": next}, e
}
func ImportSample(ctx context.Context, owner int64, id, key string, revision int, input map[string]any, snapshots []collector.Document, expected []map[string]any) (*Operation, error) {
	if len(snapshots) == 0 || len(expected) > 100 || len(collector.JSON(expected)) > 1024*1024 {
		return nil, collector.Invalid("sample", "需要1..3页快照，预期最多100条/1MiB")
	}
	if e := ValidateDocuments(snapshots); e != nil {
		return nil, e
	}
	return Mutate(ctx, owner, "importSample", id, key, map[string]any{"revision": revision, "input": input, "snapshots": snapshots, "expected_outputs": expected}, false, func(s *xorm.Session, o *Operation) (any, error) {
		c, e := getCollector(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if c.Archived || c.Revision != revision {
			return nil, conflict("草稿已变化或归档")
		}
		d, e := collector.Decode([]byte(c.Definition))
		if e != nil {
			return nil, e
		}
		input, e = collector.ValidateInput(d, input)
		if e != nil {
			return nil, e
		}
		count, e := s.Where("owner_id=? AND collector_id=?", owner, id).Count(new(Sample))
		if e != nil {
			return nil, e
		}
		if count >= 20 {
			return nil, conflict("每采集器最多20个样例")
		}
		for i, doc := range snapshots {
			if e = collector.CheckOrigin(doc.URL, d.Fetch.AllowedOrigins); e != nil {
				return nil, e
			}
			content, err := capture.Sanitize(doc.Content, doc.Format, "")
			if err != nil {
				return nil, collector.Invalid("snapshots", "快照无法解析")
			}
			snapshots[i].Content = content
			snapshots[i].PageStateID = ""
		}
		if expected == nil {
			expected = []map[string]any{}
		}
		row := Sample{ID: uuid.NewString(), OwnerID: owner, CollectorID: id, Revision: revision, Input: collector.JSON(input), Snapshots: collector.JSON(snapshots), ExpectedOutputs: collector.JSON(expected), CreatedAt: time.Now()}
		if _, e = s.Insert(&row); e != nil {
			return nil, e
		}
		return row.View(), nil
	})
}
func DeleteTest(ctx context.Context, owner int64, id string) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	o := new(Operation)
	ok, e := s.SQL("SELECT * FROM co_operations WHERE owner_id=? AND id=? AND operation='testCollector' FOR UPDATE", owner, id).Get(o)
	if e != nil {
		return e
	}
	if !ok {
		return ErrNotFound
	}
	if o.Status == "running" || o.Status == "queued" {
		return conflict("活动测试不能删除")
	}
	count, e := s.Where("evidence_id=?", id).Count(new(Version))
	if e != nil {
		return e
	}
	if count > 0 {
		return conflict("已发布版本引用此测试，不能删除")
	}
	_, e = s.Exec("DELETE FROM co_operations WHERE owner_id=? AND id=?", owner, id)
	if e != nil {
		return e
	}
	return s.Commit()
}
func DeleteSample(ctx context.Context, owner int64, id string) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	row := new(Sample)
	ok, e := s.SQL("SELECT * FROM co_samples WHERE owner_id=? AND id=? FOR UPDATE", owner, id).Get(row)
	if e != nil {
		return e
	}
	if !ok {
		return ErrNotFound
	}
	_, e = s.Exec("DELETE FROM co_samples WHERE owner_id=? AND id=?", owner, id)
	if e != nil {
		return e
	}
	return s.Commit()
}
