package co

import (
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func TestCOO4FiniteSources(t *testing.T) {
	rows, e := parseSourceFile("csv", "url,label\nhttps://example.test/a,\"a,b\"\nhttps://example.test/b,b\n")
	if e != nil || len(rows) != 2 || rows[0]["label"] != "a,b" {
		t.Fatal(rows, e)
	}
	for _, content := range []string{"url,url\na,b", "url,label\na", ""} {
		if _, e = parseSourceFile("csv", content); e == nil {
			t.Fatal("invalid CSV accepted")
		}
	}
	for _, content := range []string{"[]", "[null]", "[1]"} {
		if _, e = parseSourceFile("json", content); e == nil {
			t.Fatal("invalid object array accepted")
		}
	}
	src := ManualSource{Kind: "table", SnapshotID: "s", SnapshotHash: "h", Count: 2}
	if e = validateTaskSource(src); e != nil {
		t.Fatal(e)
	}
	src.Inputs = []map[string]any{{"url": "https://example.test"}}
	if validateTaskSource(src) == nil {
		t.Fatal("mixed latest/manual source accepted")
	}
}
func TestCOO4ReplayScopeAndSchema(t *testing.T) {
	m := TaskManifest{Definition: TaskDefinition{EntryStage: "list", Stages: []TaskStage{{ID: "list"}, {ID: "detail", RefreshSeconds: 30}, {ID: "comments"}, {ID: "branch"}}, Edges: []TaskEdge{{From: "list", To: "detail"}, {From: "detail", To: "comments"}, {From: "list", To: "branch"}}}, Stages: map[string]FrozenStage{"list": {}, "detail": {}, "comments": {}, "branch": {}}}
	replay, skip, e := pruneManifest(m, "detail")
	if e != nil || len(replay.Definition.Stages) != 2 || len(replay.Definition.Edges) != 1 || len(skip) != 2 || replay.Definition.EntryStage != "detail" || replay.Stage("detail").RefreshSeconds != 0 {
		t.Fatal("incorrect replay scope", replay, skip, e)
	}
	if len(m.Stages) != 4 {
		t.Fatal("pruning mutated historical manifests")
	}
	f := collector.Field{Key: "id", Type: "string", Required: true}
	old := TableSchema{Fields: []collector.Field{f}, UniqueKey: []string{"id"}}
	next := TableSchema{Fields: []collector.Field{f, {Key: "label", Type: "string"}}, UniqueKey: []string{"id"}}
	if e = schemaCompatibility(old, next); e != nil {
		t.Fatal(e)
	}
	next.Fields[1].Required = true
	if schemaCompatibility(old, next) == nil {
		t.Fatal("required new field accepted")
	}
	next.Fields = []collector.Field{}
	if schemaCompatibility(old, next) == nil {
		t.Fatal("removed old field accepted")
	}
}
