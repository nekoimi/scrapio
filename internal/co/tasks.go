package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"strings"
	"time"
	"xorm.io/xorm"
)

type Task struct {
	ID         string    `xorm:"id pk" json:"id"`
	OwnerID    int64     `xorm:"owner_id" json:"-"`
	Name       string    `json:"name"`
	Revision   int       `json:"revision"`
	Definition string    `xorm:"jsonb definition" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Task) TableName() string { return "co_tasks" }
func (t Task) View() any {
	return map[string]any{"id": t.ID, "name": t.Name, "revision": t.Revision, "definition": json.RawMessage(t.Definition), "created_at": t.CreatedAt, "updated_at": t.UpdatedAt}
}

type TaskVersion struct {
	ID                 string `xorm:"id pk"`
	OwnerID            int64  `xorm:"owner_id"`
	TaskID             string `xorm:"task_id"`
	Number             int
	Revision           int
	CollectorVersionID string `xorm:"collector_version_id"`
	Manifest           string `xorm:"jsonb manifest"`
	ManifestHash       string `xorm:"manifest_hash"`
	CreatedAt          time.Time
}

func (TaskVersion) TableName() string { return "co_task_versions" }

type DataTable struct {
	ID            string `xorm:"id pk"`
	OwnerID       int64  `xorm:"owner_id"`
	Name          string
	CurrentSchema int `xorm:"current_schema_version"`
	CreatedAt     time.Time
}

func (DataTable) TableName() string { return "co_data_tables" }

type SchemaVersion struct {
	TableID    string `xorm:"table_id"`
	Version    int
	Schema     string `xorm:"jsonb schema"`
	SchemaHash string `xorm:"schema_hash"`
}

func (SchemaVersion) TableName() string { return "co_schema_versions" }
func getTask(s *xorm.Session, owner int64, id string, lock bool) (*Task, error) {
	t := new(Task)
	q := "SELECT * FROM co_tasks WHERE owner_id=? AND id=?"
	if lock {
		q += " FOR UPDATE"
	}
	ok, e := s.SQL(q, owner, id).Get(t)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}
func GetTask(ctx context.Context, owner int64, id string) (*Task, error) {
	s := session(ctx)
	defer s.Close()
	return getTask(s, owner, id, false)
}
func collectorVersion(s *xorm.Session, owner int64, id string) (Version, collector.Definition, error) {
	v := Version{}
	ok, e := s.Where("owner_id=? AND id=?", owner, id).Get(&v)
	if e != nil {
		return v, collector.Definition{}, e
	}
	if !ok {
		return v, collector.Definition{}, ErrNotFound
	}
	d, e := collector.Decode([]byte(v.Definition))
	return v, d, e
}
func tableSchema(s *xorm.Session, owner int64, b SaveBinding, lock bool) (TableSchema, error) {
	t := new(DataTable)
	q := "SELECT * FROM co_data_tables WHERE owner_id=? AND id=?"
	if lock {
		q += " FOR SHARE"
	}
	ok, e := s.SQL(q, owner, b.TableID).Get(t)
	if e != nil {
		return TableSchema{}, e
	}
	if !ok {
		return TableSchema{}, ErrNotFound
	}
	if t.CurrentSchema != b.SchemaVersion {
		return TableSchema{}, collector.Fault("SCHEMA_CONFLICT", "save", "绑定的Schema已不是当前版本，请重新预演")
	}
	sv := new(SchemaVersion)
	ok, e = s.Where("table_id=? AND version=?", b.TableID, b.SchemaVersion).Get(sv)
	if e != nil {
		return TableSchema{}, e
	}
	if !ok || sv.SchemaHash != b.SchemaHash {
		return TableSchema{}, collector.Fault("SCHEMA_CONFLICT", "save", "表结构哈希不匹配")
	}
	var schema TableSchema
	e = collector.Strict([]byte(sv.Schema), &schema)
	return schema, e
}
func buildManifest(s *xorm.Session, owner int64, d TaskDefinition) (TaskManifest, error) {
	m := TaskManifest{Definition: d}
	if e := d.Validate(); e != nil {
		return m, e
	}
	v, cd, e := collectorVersion(s, owner, d.Stages[0].CollectorVersionID)
	if e != nil {
		return m, e
	}
	schema, e := tableSchema(s, owner, d.Stages[0].Save, true)
	if e != nil {
		return m, e
	}
	if e = validateBinding(d.Stages[0].Save, schema, cd); e != nil {
		return m, e
	}
	rows, e := PreviewInputs(cd, d.Source.Inputs)
	if e != nil {
		return m, e
	}
	for _, row := range rows {
		if row.Error != nil {
			return m, &collector.Error{Code: "INPUT_FIELD_INVALID", Message: fmt.Sprintf("输入第%d行：%s", row.Ordinal+1, row.Error.Message), Location: row.Error.Location}
		}
		d.Source.Inputs[row.Ordinal] = row.Values
	}
	m.Definition = d
	m.Collector = freezeCollector(v)
	m.CollectorDefinition = cd
	m.Schema = schema
	m.Hash = collector.Hash(m)
	return m, nil
}
func CreateTask(ctx context.Context, owner int64, name, key string, d TaskDefinition) (*Operation, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return nil, collector.Invalid("name", "名称需要1..64个字符")
	}
	return Mutate(ctx, owner, "createTask", "", key, map[string]any{"name": name, "definition": d}, false, func(s *xorm.Session, o *Operation) (any, error) {
		m, e := buildManifest(s, owner, d)
		if e != nil {
			return nil, e
		}
		t := Task{ID: uuid.NewString(), OwnerID: owner, Name: name, Revision: 1, Definition: collector.JSON(m.Definition), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		_, e = s.Insert(&t)
		return t.View(), e
	})
}
func SaveTask(ctx context.Context, owner int64, id string, revision int, d TaskDefinition) (*Task, error) {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	t, e := getTask(s, owner, id, true)
	if e != nil {
		return nil, e
	}
	if t.Revision != revision {
		return nil, &collector.Error{Code: "REVISION_CONFLICT", Message: "任务草稿已变化，请保留本地修改后重新加载", Details: map[string]any{"latest_revision": t.Revision}}
	}
	m, e := buildManifest(s, owner, d)
	if e != nil {
		return nil, e
	}
	if t.Definition != collector.JSON(m.Definition) {
		t.Revision++
		t.Definition = collector.JSON(m.Definition)
		t.UpdatedAt = time.Now()
		if _, e = s.ID(id).Cols("revision", "definition", "updated_at").Update(t); e != nil {
			return nil, e
		}
	}
	return t, s.Commit()
}
func PublishTask(ctx context.Context, owner int64, id, key string, revision int) (*Operation, error) {
	return Mutate(ctx, owner, "publishTask", id, key, map[string]int{"expected_revision": revision}, false, func(s *xorm.Session, o *Operation) (any, error) {
		t, e := getTask(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if t.Revision != revision {
			return nil, conflict("任务草稿revision已变化")
		}
		var d TaskDefinition
		if e = collector.Strict([]byte(t.Definition), &d); e != nil {
			return nil, e
		}
		m, e := buildManifest(s, owner, d)
		if e != nil {
			return nil, e
		}
		v := new(TaskVersion)
		ok, e := s.Where("task_id=? AND revision=?", id, revision).Get(v)
		if e != nil {
			return nil, e
		}
		if !ok {
			count, e := s.Where("task_id=?", id).Count(new(TaskVersion))
			if e != nil {
				return nil, e
			}
			v = &TaskVersion{ID: uuid.NewString(), OwnerID: owner, TaskID: id, Number: int(count) + 1, Revision: revision, CollectorVersionID: d.Stages[0].CollectorVersionID, Manifest: collector.JSON(m), ManifestHash: m.Hash, CreatedAt: time.Now()}
			if _, e = s.Insert(v); e != nil {
				return nil, e
			}
		}
		return map[string]any{"id": v.ID, "number": v.Number, "revision": v.Revision, "manifest": json.RawMessage(v.Manifest)}, nil
	})
}
func CreateDataTable(ctx context.Context, owner int64, name, key string, schema TableSchema) (*Operation, error) {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 64 {
		return nil, collector.Invalid("name", "名称需要1..64个字符")
	}
	if e := schema.Validate(); e != nil {
		return nil, e
	}
	return Mutate(ctx, owner, "createTable", "", key, map[string]any{"name": name, "schema": schema}, false, func(s *xorm.Session, o *Operation) (any, error) {
		t := DataTable{ID: uuid.NewString(), OwnerID: owner, Name: name, CurrentSchema: 1, CreatedAt: time.Now()}
		if _, e := s.Insert(&t); e != nil {
			return nil, e
		}
		sv := SchemaVersion{TableID: t.ID, Version: 1, Schema: collector.JSON(schema), SchemaHash: collector.Hash(schema)}
		_, e := s.Insert(&sv)
		return map[string]any{"id": t.ID, "name": t.Name, "schema_version": 1, "schema_hash": sv.SchemaHash, "schema": schema}, e
	})
}

// Views are bounded projections; owner and lease credentials never leave the API.
func views(s *xorm.Session, q string, args ...any) ([]json.RawMessage, error) {
	rows, e := s.QueryString(append([]any{"SELECT (to_jsonb(co_view) - 'owner_id' - 'lease_token' - 'values_json')::text AS value FROM (" + q + ") co_view"}, args...)...)
	if e != nil {
		return nil, e
	}
	out := []json.RawMessage{}
	for _, row := range rows {
		out = append(out, json.RawMessage(row["value"]))
	}
	return out, nil
}
func TaskList(ctx context.Context, owner int64, cursor string, limit int) (any, error) {
	return domainList(ctx, owner, "co_tasks", cursor, limit)
}
func domainList(ctx context.Context, owner int64, table, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	projection := "*"
	if table == "co_tasks" {
		projection = "id,name,revision,created_at,updated_at,jsonb_array_length(definition -> 'source' -> 'inputs') AS input_count,definition -> 'stages' -> 0 -> 'save' ->> 'table_id' AS table_id,definition -> 'stages' -> 0 -> 'save' ->> 'schema_version' AS schema_version"
	}
	if table == "co_runs" {
		projection = "id,task_version_id,namespace,status,control_epoch,preparing_cursor,input_count,output_count,event_seq,retry_of,retry_scope,created_at,finished_at"
	}
	q := "SELECT " + projection + " FROM " + table + " WHERE owner_id=?"
	args := []any{owner}
	if cursor != "" {
		b, e := decodeCursor(cursor)
		if e != nil {
			return nil, e
		}
		q += " AND (created_at,id)<(?,?)"
		args = append(args, b.At, b.ID)
	}
	q += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, e := views(s, q, args...)
	if e != nil {
		return nil, e
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		var last struct {
			ID        string    `json:"id"`
			CreatedAt time.Time `json:"created_at"`
		}
		_ = json.Unmarshal(rows[limit-1], &last)
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return map[string]any{"items": rows, "next_cursor": next}, nil
}
func TaskVersions(ctx context.Context, owner int64, id string) (any, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := getTask(s, owner, id, false); e != nil {
		return nil, e
	}
	rows, e := views(s, "SELECT id,number,revision,manifest,created_at FROM co_task_versions WHERE owner_id=? AND task_id=? ORDER BY number DESC LIMIT 100", owner, id)
	return map[string]any{"items": rows}, e
}
func Tables(ctx context.Context, owner int64, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	q := `SELECT t.*,s.version AS schema_version,s.schema_hash,s.schema FROM co_data_tables t JOIN co_schema_versions s ON s.table_id=t.id AND s.version=t.current_schema_version WHERE t.owner_id=?`
	args := []any{owner}
	if cursor != "" {
		b, e := decodeCursor(cursor)
		if e != nil {
			return nil, e
		}
		q += " AND (t.created_at,t.id)<(?,?)"
		args = append(args, b.At, b.ID)
	}
	q += " ORDER BY t.created_at DESC,t.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, e := views(s, q, args...)
	if e != nil {
		return nil, e
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		var t DataTable
		_ = json.Unmarshal(rows[limit-1], &t)
		next = encodeCursor(t.CreatedAt, t.ID)
	}
	return map[string]any{"items": rows, "next_cursor": next}, nil
}
func TaskInputPreview(ctx context.Context, owner int64, version string, inputs []map[string]any) (any, error) {
	s := session(ctx)
	defer s.Close()
	_, d, e := collectorVersion(s, owner, version)
	if e != nil {
		return nil, e
	}
	rows, e := PreviewInputs(d, inputs)
	return map[string]any{"items": rows}, e
}

func CollectorVersion(ctx context.Context, owner int64, id string) (any, error) {
	s := session(ctx)
	defer s.Close()
	v, _, e := collectorVersion(s, owner, id)
	if e != nil {
		return nil, e
	}
	return v.View(), nil
}
func TableView(ctx context.Context, owner int64, id string) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows, e := views(s, `SELECT t.*,s.version AS schema_version,s.schema_hash,s.schema FROM co_data_tables t JOIN co_schema_versions s ON s.table_id=t.id AND s.version=t.current_schema_version WHERE t.owner_id=? AND t.id=?`, owner, id)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}
