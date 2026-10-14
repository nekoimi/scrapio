package co

import (
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func TestCOSubscriptionCycle(t *testing.T) {
	edges := map[string][]string{"a": {"b"}, "b": {"c"}}
	if !subscriptionCycle(edges, "c", []string{"a"}) || !subscriptionCycle(edges, "a", []string{"a"}) {
		t.Fatal("feedback accepted")
	}
	if subscriptionCycle(edges, "a", []string{"d"}) {
		t.Fatal("independent destination rejected")
	}
}
func TestCOSubscriptionTypedMapping(t *testing.T) {
	schema := TableSchema{Fields: []collector.Field{{Key: "target", Type: "url"}, {Key: "id", Type: "string"}}}
	d := collector.Definition{InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}, {Key: "movie_id", Type: "string", Required: true}}}
	if e := subscriptionMapping(schema, d, map[string]string{"url": "target", "movie_id": "id"}); e != nil {
		t.Fatal(e)
	}
	if e := subscriptionMapping(schema, d, map[string]string{"url": "id", "movie_id": "id"}); e == nil {
		t.Fatal("string silently becomes URL")
	}
	if e := subscriptionMapping(schema, d, map[string]string{"url": "target"}); e == nil {
		t.Fatal("required input missing")
	}
}
