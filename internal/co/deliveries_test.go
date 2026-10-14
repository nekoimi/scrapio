package co

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/plugin"
	"testing"
)

type receiptFixture struct {
	submits, queries  int
	outcome           string
	lost, panicSubmit bool
	destination       string
}

func (*receiptFixture) Code() string                 { return "aria2" }
func (*receiptFixture) Capabilities() []string       { return nil }
func (*receiptFixture) Health(context.Context) error { return nil }
func (*receiptFixture) Handle(context.Context, plugin.Task) (any, string, error) {
	panic("legacy handle must not be called")
}
func (*receiptFixture) DeliveryVersion() string { return plugin.Aria2DeliveryVersion }
func (f *receiptFixture) DeliveryDestination() string {
	if f.destination != "" {
		return f.destination
	}
	return "fixture-service"
}
func (*receiptFixture) DeliveryIdentity(plugin.Task) string { return "fixture-gid" }
func (f *receiptFixture) SubmitDelivery(context.Context, plugin.Task) (any, string, error) {
	f.submits++
	if f.panicSubmit {
		panic("fixture")
	}
	if f.lost {
		return nil, "fixture-gid", errors.New("lost response")
	}
	return map[string]any{"gid": "fixture-gid"}, "fixture-gid", nil
}
func (f *receiptFixture) QueryDelivery(_ context.Context, _ plugin.Task, id string) (any, string, error) {
	f.queries++
	if id != "fixture-gid" {
		return nil, "unknown", errors.New("wrong identity")
	}
	return map[string]any{"gid": id}, f.outcome, nil
}

func TestCODeliveryOriginalReceipt(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		recovery, lost, panics, changed bool
		outcome, want                   string
		submits, queries                int
	}{
		{name: "accepted", want: "succeeded", submits: 1},
		{name: "lost accepted", lost: true, outcome: "accepted", want: "succeeded", submits: 1, queries: 1},
		{name: "lost missing", lost: true, outcome: "unknown", want: "awaiting_confirmation", submits: 1, queries: 1},
		{name: "restart", recovery: true, outcome: "accepted", want: "succeeded", queries: 1},
		{name: "restart missing", recovery: true, outcome: "unknown", want: "awaiting_confirmation", queries: 1},
		{name: "external failed", recovery: true, outcome: "failed", want: "failed", queries: 1},
		{name: "panic", panics: true, want: "awaiting_confirmation", submits: 1},
		{name: "different service", changed: true, recovery: true, want: "awaiting_confirmation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &receiptFixture{lost: tc.lost, panicSubmit: tc.panics, outcome: tc.outcome}
			if tc.changed {
				f.destination = "new-service"
			}
			c := &DeliveryClaim{Delivery: Delivery{PluginVersion: plugin.Aria2DeliveryVersion, ExternalID: "fixture-gid", DestinationHash: "fixture-service", Input: `{"url":"https://example.test/file"}`}, Recovery: tc.recovery, Handler: f}
			status, _, _ := runDelivery(context.Background(), c)
			if status != tc.want || f.submits != tc.submits || f.queries != tc.queries {
				t.Fatalf("status=%s submits=%d queries=%d", status, f.submits, f.queries)
			}
		})
	}
}

func TestCODeliveryMappingAndPolicies(t *testing.T) {
	d := collector.Definition{Output: collector.Output{Port: "records", Fields: []collector.Field{{Key: "url", Type: "url", Required: true}}}}
	b := DeliveryBinding{ID: "download", Port: "records", PluginCode: "aria2", Version: plugin.Aria2DeliveryVersion, Emit: "valid", Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output.url"}}}
	stage := TaskStage{ID: "entry", Save: SaveBinding{Mode: "pass_through"}, Deliveries: []DeliveryBinding{b}}
	if e := validateDeliveries(stage, d); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(*TaskStage, *collector.Definition)
	}{
		{"invalid port", func(s *TaskStage, _ *collector.Definition) { s.Deliveries[0].Port = "missing" }},
		{"cloud not ready", func(s *TaskStage, _ *collector.Definition) { s.Deliveries[0].PluginCode = "cloud-driver" }},
		{"wrong version", func(s *TaskStage, _ *collector.Definition) { s.Deliveries[0].Version = "next" }},
		{"pass cannot changed", func(s *TaskStage, _ *collector.Definition) { s.Deliveries[0].Emit = "changed" }},
		{"array", func(_ *TaskStage, d *collector.Definition) { d.Output.Fields[0].Multiple = true }},
		{"optional", func(_ *TaskStage, d *collector.Definition) { d.Output.Fields[0].Required = false }},
		{"nullable", func(_ *TaskStage, d *collector.Definition) { d.Output.Fields[0].Nullable = true }},
		{"duplicate", func(s *TaskStage, _ *collector.Definition) { s.Deliveries = append(s.Deliveries, s.Deliveries[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s TaskStage
			var def collector.Definition
			_ = json.Unmarshal([]byte(collector.JSON(stage)), &s)
			_ = json.Unmarshal([]byte(collector.JSON(d)), &def)
			tc.change(&s, &def)
			if validateDeliveries(s, def) == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	if _, e := deliveryInput(b, map[string]any{"url": "https://example.test/file"}); e != nil {
		t.Fatal(e)
	}
	for _, v := range []any{nil, []string{"https://example.test"}, "javascript:alert(1)", "https://user:pass@example.test/file"} {
		if _, e := deliveryInput(b, map[string]any{"url": v}); e == nil {
			t.Fatal("invalid runtime input accepted", v)
		}
	}
	if !emitOutput("changed", "created") || !emitOutput("changed", "updated") || emitOutput("changed", "unchanged") || emitOutput("created", "updated") || !emitOutput("valid", "passed") {
		t.Fatal("emission policy changed")
	}
}
