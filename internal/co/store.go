// Package co is the independent collector control plane. All persistence stays in co_ tables.
package co

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"strings"
	"time"
	"xorm.io/xorm"
)

var ErrNotFound = errors.New("resource not found")

func conflict(message string) error { return &collector.Error{Code: "CONFLICT", Message: message} }

type Collector struct {
	ID         string    `xorm:"id pk" json:"id"`
	OwnerID    int64     `xorm:"owner_id" json:"-"`
	Name       string    `json:"name"`
	Revision   int       `json:"revision"`
	Definition string    `xorm:"jsonb definition" json:"-"`
	Archived   bool      `json:"archived"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Collector) TableName() string { return "co_collectors" }

type Operation struct {
	ID          string     `xorm:"id pk" json:"id"`
	OwnerID     int64      `xorm:"owner_id" json:"-"`
	ResourceID  string     `xorm:"resource_id" json:"resource_id"`
	Operation   string     `json:"operation"`
	Key         string     `json:"-"`
	Fingerprint string     `json:"-"`
	Status      string     `json:"status"`
	Payload     string     `xorm:"jsonb payload" json:"-"`
	Result      string     `xorm:"jsonb result" json:"-"`
	Error       string     `xorm:"jsonb error" json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

func (Operation) TableName() string { return "co_operations" }
func (o Operation) View() map[string]any {
	return map[string]any{"id": o.ID, "resource_id": o.ResourceID, "operation": o.Operation, "status": o.Status, "result": json.RawMessage(o.Result), "error": json.RawMessage(o.Error), "created_at": o.CreatedAt, "finished_at": o.FinishedAt}
}
func (c Collector) View() map[string]any {
	return map[string]any{"id": c.ID, "name": c.Name, "revision": c.Revision, "archived": c.Archived, "definition": json.RawMessage(c.Definition), "created_at": c.CreatedAt, "updated_at": c.UpdatedAt}
}

type Sample struct {
	ID              string `xorm:"id pk"`
	OwnerID         int64  `xorm:"owner_id"`
	CollectorID     string `xorm:"collector_id"`
	Revision        int
	Input           string `xorm:"jsonb input"`
	Snapshots       string `xorm:"jsonb snapshots"`
	ExpectedOutputs string `xorm:"jsonb expected_outputs"`
	CreatedAt       time.Time
}

func (Sample) TableName() string { return "co_samples" }
func (s Sample) View() any {
	return map[string]any{"id": s.ID, "revision": s.Revision, "input": json.RawMessage(s.Input), "snapshots": json.RawMessage(s.Snapshots), "expected_outputs": json.RawMessage(s.ExpectedOutputs), "created_at": s.CreatedAt}
}

type Version struct {
	ID                 string `xorm:"id pk"`
	OwnerID            int64  `xorm:"owner_id"`
	CollectorID        string `xorm:"collector_id"`
	Number             int
	Revision           int
	Definition         string `xorm:"jsonb definition"`
	DefinitionHash     string `xorm:"definition_hash"`
	InputSchemaHash    string `xorm:"input_schema_hash"`
	OutputSchemaHash   string `xorm:"output_schema_hash"`
	InterpreterVersion string `xorm:"interpreter_version"`
	PublishVersion     string `xorm:"publish_version"`
	Requirements       string `xorm:"jsonb requirements"`
	EvidenceID         string `xorm:"evidence_id"`
	CheckID            string `xorm:"check_id"`
	CreatedAt          time.Time
}

func (Version) TableName() string { return "co_collector_versions" }
func (v Version) View() any {
	return map[string]any{"id": v.ID, "collector_id": v.CollectorID, "number": v.Number, "revision": v.Revision, "definition": json.RawMessage(v.Definition), "definition_hash": v.DefinitionHash, "input_schema_hash": v.InputSchemaHash, "output_schema_hash": v.OutputSchemaHash, "interpreter_version": v.InterpreterVersion, "publish_version": v.PublishVersion, "requirements": json.RawMessage(v.Requirements), "evidence_id": v.EvidenceID, "created_at": v.CreatedAt}
}

type Session struct {
	ID          string `xorm:"id pk"`
	OwnerID     int64  `xorm:"owner_id"`
	CollectorID string `xorm:"collector_id"`
	Revision    int
	Definition  string `xorm:"jsonb definition"`
	Input       string `xorm:"jsonb input"`
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

func (Session) TableName() string { return "co_sessions" }

var databaseEngine = db.Instance

func session(ctx context.Context) *xorm.Session { return databaseEngine().NewSession().Context(ctx) }
func getCollector(s *xorm.Session, owner int64, id string, lock bool) (*Collector, error) {
	c := new(Collector)
	q := "SELECT * FROM co_collectors WHERE owner_id=? AND id=?"
	if lock {
		q += " FOR UPDATE"
	}
	ok, e := s.SQL(q, owner, id).Get(c)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}
func Get(ctx context.Context, owner int64, id string) (*Collector, error) {
	s := session(ctx)
	defer s.Close()
	return getCollector(s, owner, id, false)
}
func List(ctx context.Context, owner int64, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows := []struct {
		Collector     `xorm:"extends"`
		VersionNumber int    `xorm:"version_number"`
		LatestTest    string `xorm:"latest_test"`
	}{}
	query := `SELECT c.*,COALESCE((SELECT MAX(number) FROM co_collector_versions v WHERE v.collector_id=c.id),0) AS version_number,
 COALESCE((SELECT jsonb_build_object('id',o.id,'status',o.status,'result_status',o.result->>'status','mode',o.result->>'mode')::text FROM co_operations o WHERE o.resource_id=c.id AND o.owner_id=c.owner_id AND o.operation='testCollector' ORDER BY o.created_at DESC LIMIT 1),'{}') AS latest_test FROM co_collectors c WHERE c.owner_id=?`
	args := []any{owner}
	if cursor != "" {
		boundary, e := decodeCursor(cursor)
		if e != nil {
			return nil, e
		}
		query += " AND (c.created_at,c.id)<(?,?)"
		args = append(args, boundary.At, boundary.ID)
	}
	query += " ORDER BY c.created_at DESC,c.id DESC LIMIT ?"
	args = append(args, limit+1)
	e := s.SQL(query, args...).Find(&rows)
	if e != nil {
		return nil, e
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	items := []any{}
	for _, r := range rows {
		view := r.View()
		view["version_number"] = r.VersionNumber
		view["latest_test"] = json.RawMessage(r.LatestTest)
		items = append(items, view)
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}
func GetOperation(ctx context.Context, owner int64, id string) (*Operation, error) {
	s := session(ctx)
	defer s.Close()
	o := new(Operation)
	ok, e := s.Where("owner_id=? AND id=?", owner, id).Get(o)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	return o, nil
}
func ByKey(ctx context.Context, owner int64, operation, resource, key string) (*Operation, error) {
	s := session(ctx)
	defer s.Close()
	o := new(Operation)
	ok, e := s.Where("owner_id=? AND operation=? AND resource_id=? AND key=?", owner, operation, resource, key).Get(o)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	return o, nil
}

// Mutate journals owner+operation+resource+key and performs only short database work.
func Mutate(ctx context.Context, owner int64, operation, resource, key string, payload any, queued bool, fn func(*xorm.Session, *Operation) (any, error)) (*Operation, error) {
	if key == "" || len(key) > 128 || strings.ContainsAny(key, "\r\n") {
		return nil, collector.Invalid("Idempotency-Key", "需要1..128字符幂等键")
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	if e := idempotency.Lock(s, owner, "co:"+operation+":"+resource, key); e != nil {
		return nil, e
	}
	o := new(Operation)
	ok, e := s.Where("owner_id=? AND operation=? AND resource_id=? AND key=?", owner, operation, resource, key).Get(o)
	if e != nil {
		return nil, e
	}
	fingerprint := collector.Hash(payload)
	if ok {
		if o.Fingerprint != fingerprint {
			return nil, &collector.Error{Code: "IDEMPOTENCY_CONFLICT", Message: "同一幂等键已用于不同参数，请查询原操作", Details: map[string]any{"operation_id": o.ID}}
		}
		return o, s.Commit()
	}
	if queued {
		if e = idempotency.Lock(s, owner, "co:pending-capacity", "owner"); e != nil {
			return nil, e
		}
		count, e := s.Where("owner_id=? AND status IN ('queued','running')", owner).Count(new(Operation))
		if e != nil {
			return nil, e
		}
		if count >= 10 {
			return nil, conflict("最多10个待处理操作，请等待当前操作完成")
		}
	}
	o = &Operation{ID: uuid.NewString(), OwnerID: owner, ResourceID: resource, Operation: operation, Key: key, Fingerprint: fingerprint, Payload: collector.JSON(payload), Result: "{}", Error: "{}", Status: "succeeded", CreatedAt: time.Now()}
	if queued {
		o.Status = "queued"
	}
	if fn != nil {
		value, e := fn(s, o)
		if e != nil {
			return nil, e
		}
		if value != nil {
			o.Result = collector.JSON(value)
		}
	}
	if _, e = s.Insert(o); e != nil {
		return nil, e
	}
	return o, s.Commit()
}
func Create(ctx context.Context, owner int64, name, key string) (*Operation, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return nil, collector.Invalid("name", "名称需要1..64个字符")
	}
	return Mutate(ctx, owner, "createCollector", "", key, map[string]string{"name": name}, false, func(s *xorm.Session, o *Operation) (any, error) {
		c := Collector{ID: uuid.NewString(), OwnerID: owner, Name: name, Definition: "{}", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if _, e := s.Insert(&c); e != nil {
			return nil, e
		}
		return c.View(), nil
	})
}
func Save(ctx context.Context, owner int64, id string, revision int, d collector.Definition) (*Collector, error) {
	if revision < 0 {
		return nil, collector.Invalid("expected_revision", "revision不能为负数")
	}
	if e := d.Validate(); e != nil {
		return nil, e
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	c, e := getCollector(s, owner, id, true)
	if e != nil {
		return nil, e
	}
	if c.Archived {
		return nil, conflict("采集器已归档")
	}
	if c.Revision != revision {
		return nil, &collector.Error{Code: "REVISION_CONFLICT", Message: fmt.Sprintf("草稿版本冲突，服务器revision为%d；请保留本地修改再加载", c.Revision), Location: map[string]string{"field": "expected_revision"}, Details: map[string]any{"latest_revision": c.Revision}}
	}
	if previous, err := collector.Decode([]byte(c.Definition)); err == nil && collector.Hash(previous) == collector.Hash(d) {
		return c, s.Commit()
	}
	c.Revision++
	c.Definition = collector.JSON(d)
	c.UpdatedAt = time.Now()
	_, e = s.ID(id).Cols("revision", "definition", "updated_at").Update(c)
	if e != nil {
		return nil, e
	}
	return c, s.Commit()
}

type TestPayload struct {
	Revision   int                  `json:"expected_revision"`
	Definition collector.Definition `json:"definition"`
	Input      map[string]any       `json:"input"`
	Mode       string               `json:"mode"`
	SampleID   string               `json:"sample_id,omitempty"`
	Snapshots  []collector.Document `json:"snapshots,omitempty"`
	Expected   []map[string]any     `json:"expected_outputs,omitempty"`
	EvidenceID string               `json:"evidence_id,omitempty"`
	TTL        int                  `json:"ttl_seconds,omitempty"`
	Width      int                  `json:"width,omitempty"`
	Height     int                  `json:"height,omitempty"`
}

func Queue(ctx context.Context, owner int64, id, key, kind string, p TestPayload) (*Operation, error) {
	requestFingerprint := p
	p.Definition = collector.Definition{}
	return Mutate(ctx, owner, kind, id, key, requestFingerprint, true, func(s *xorm.Session, o *Operation) (any, error) {
		c, e := getCollector(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if c.Archived || c.Revision != p.Revision {
			return nil, conflict("草稿已变化或采集器归档；请先保存再执行")
		}
		p.Definition, e = collector.Decode([]byte(c.Definition))
		if e != nil {
			return nil, e
		}
		if kind == "testCollector" || kind == "createEditorSession" {
			p.Input, e = collector.ValidateInput(p.Definition, p.Input)
			if e != nil {
				return nil, e
			}
			if _, e = collector.Prepare(p.Definition, p.Input); e != nil {
				return nil, e
			}
		}
		if kind == "testCollector" {
			if e = idempotency.Lock(s, owner, "co:test-capacity", "owner"); e != nil {
				return nil, e
			}
			if p.Mode != "live" && p.Mode != "offline" {
				return nil, collector.Invalid("mode", "需要live/offline")
			}
			if p.Mode == "offline" {
				sample := new(Sample)
				ok, e := s.Where("owner_id=? AND collector_id=? AND id=?", owner, id, p.SampleID).Get(sample)
				if e != nil {
					return nil, e
				}
				if !ok {
					return nil, ErrNotFound
				}
				if collector.Hash(p.Input) != collector.Hash(jsonValue(sample.Input)) {
					return nil, collector.Invalid("input", "离线回归必须使用样例冻结输入")
				}
				_ = json.Unmarshal([]byte(sample.Snapshots), &p.Snapshots)
				_ = json.Unmarshal([]byte(sample.ExpectedOutputs), &p.Expected)
			}
			count, e := s.Where("owner_id=? AND operation='testCollector' AND status IN ('queued','running')", owner).Count(new(Operation))
			if e != nil {
				return nil, e
			}
			if count >= 3 {
				return nil, conflict("最多3个活动测试，请等待当前测试结束")
			}
			total, e := s.Where("owner_id=? AND resource_id=? AND operation='testCollector'", owner, id).Count(new(Operation))
			if e != nil {
				return nil, e
			}
			if total >= 100 {
				return nil, conflict("测试历史达到100条，请清理旧测试")
			}
		}
		o.Payload = collector.JSON(p)
		return nil, nil
	})
}
func jsonValue(raw string) any { var v any; _ = json.Unmarshal([]byte(raw), &v); return v }
func Finish(id, status string, result any, err error) error {
	s := session(context.Background())
	defer s.Close()
	values := map[string]any{"status": status, "result": collector.JSON(result), "finished_at": time.Now(), "error": "{}"}
	if err != nil {
		values["error"] = collector.JSON(collector.AsError(err))
	}
	_, e := s.Table("co_operations").Where("id=? AND status='running'", id).Update(values)
	return e
}
func Claim(ctx context.Context) (*Operation, error) {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	o := new(Operation)
	ok, e := s.SQL("SELECT * FROM co_operations WHERE status='queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1").Get(o)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, nil
	}
	_, e = s.Table("co_operations").Where("id=?", o.ID).Update(map[string]any{"status": "running"})
	if e != nil {
		return nil, e
	}
	o.Status = "running"
	return o, s.Commit()
}
func Recover() error {
	s := session(context.Background())
	defer s.Close()
	_, e := s.Exec("UPDATE co_operations SET status='failed', error=?::jsonb, finished_at=NOW() WHERE status='running'", collector.JSON(&collector.Error{Code: "OUTCOME_UNCERTAIN", Message: "服务在执行中重启，原操作未重放；请确认访问/动作结果"}))
	return e
}
func GetSession(ctx context.Context, owner int64, id string) (*Session, error) {
	s := session(ctx)
	defer s.Close()
	row := new(Session)
	ok, e := s.Where("owner_id=? AND id=?", owner, id).Get(row)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	if time.Now().After(row.ExpiresAt) {
		return nil, collector.Fault("SESSION_EXPIRED", "session", "会话已过期，请重新打开")
	}
	return row, nil
}
func PutSession(ctx context.Context, row *Session) error {
	s := session(ctx)
	defer s.Close()
	_, e := s.Insert(row)
	return e
}
func Versions(ctx context.Context, owner int64, id, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows := []Version{}
	q := s.Where("owner_id=? AND collector_id=?", owner, id)
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
	for _, v := range rows {
		items = append(items, v.View())
	}
	return map[string]any{"items": items, "next_cursor": next}, e
}
func Samples(ctx context.Context, owner int64, id, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows := []Sample{}
	q := s.Where("owner_id=? AND collector_id=?", owner, id)
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
func Archive(ctx context.Context, owner int64, id, key string, revision int) (*Operation, error) {
	return Mutate(ctx, owner, "archiveCollector", id, key, map[string]int{"expected_revision": revision}, false, func(s *xorm.Session, o *Operation) (any, error) {
		c, e := getCollector(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if c.Revision != revision {
			return nil, conflict("草稿版本冲突")
		}
		var active int64
		active, e = s.Where("owner_id=? AND resource_id=? AND status IN ('queued','running')", owner, id).Count(new(Operation))
		if e != nil {
			return nil, e
		}
		if active > 0 {
			return nil, conflict("存在活动操作，稍后归档")
		}
		c.Archived = true
		c.UpdatedAt = time.Now()
		_, e = s.ID(id).Cols("archived", "updated_at").Update(c)
		return c.View(), e
	})
}

// Hard deletion is not provided: versions and their test/source evidence stay immutable.
