package collector

import (
	"context"
	"testing"
)

func TestCOO5OutputPortsShareFetchAndIdentityScope(t *testing.T) {
	d, input, doc := movie(t)
	d.Fetch.Kind = "http_html"
	p := OutputPort{Records: d.Records, Output: d.Output}
	p.Output.Port = "links"
	d.AdditionalOutputs = []OutputPort{p}
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	source := &fakeSource{docs: []Document{doc}}
	r := Execute(context.Background(), d, input, "live", source)
	if r.Status != "succeeded" || len(r.Candidates) != 2 || source.opened != 1 {
		t.Fatalf("ports must share fetch: %+v opens=%d", r, source.opened)
	}
	a, b := r.Candidates[0], r.Candidates[1]
	if a.Identity != b.Identity || CandidateKey(a) == CandidateKey(b) || a.Port != "records" || b.Port != "links" {
		t.Fatal("identity must be scoped by port", a, b)
	}
	if Hash(SampleValue(a)) == Hash(SampleValue(b)) {
		t.Fatal("sample lost port")
	}
	d.Pagination.MaxRecords = 1
	r = Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Status != "limited" || len(r.Candidates) != 1 {
		t.Fatal("combined budget", r)
	}
	d.AdditionalOutputs[0].Records.Expression = ".absent"
	d.Pagination.MaxRecords = 10
	r = Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Status != "succeeded" || len(r.Candidates) != 1 {
		t.Fatal("optional empty port", r)
	}
	d.AdditionalOutputs[0].Output.Port = "records"
	if d.Validate() == nil {
		t.Fatal("reserved duplicate port accepted")
	}
	d.AdditionalOutputs[0].Output.Port = "bad-port"
	if d.Validate() == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestCOO5PrimaryEmptyControlsPagination(t *testing.T) {
	d, input, doc := movie(t)
	d.Fetch.Kind = "http_html"
	p := OutputPort{Records: d.Records, Output: d.Output}
	p.Output.Port = "links"
	d.AdditionalOutputs = []OutputPort{p}
	d.Records.Expression = ".absent"
	d.Pagination.Kind = "url"
	d.Pagination.Next = &Locator{Strategy: "css", Expression: ".next"}
	d.Pagination.EndCondition = "empty_verified"
	d.Pagination.EmptyMarker = &Locator{Strategy: "css", Expression: ".done"}
	doc.Content += `<div class="done"></div>`
	r := Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Status != "succeeded" || len(r.Candidates) != 1 || r.Candidates[0].Port != "links" {
		t.Fatal("verified primary empty should end even with extra output", r)
	}
	d.Pagination.EmptyMarker.Expression = ".not_done"
	r = Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Reason != "ZERO_RESULT_UNVERIFIED" {
		t.Fatal("extra output masked broken primary", r)
	}
}
