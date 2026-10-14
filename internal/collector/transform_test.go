package collector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func transformFixture(expression, value, expected string) *Transform {
	return &Transform{Version: TransformVersion, Input: Field{Key: "value", Type: "string", Required: true}, Expression: expression, Samples: []TransformSample{{Value: json.RawMessage(value), Expected: json.RawMessage(expected)}}}
}
func TestCOTransformTypedExpression(t *testing.T) {
	transform := transformFixture(`Number(value.replace("分", "").trim())`, `" 9.2分 "`, `9.2`)
	target := Field{Key: "rating", Type: "number", Required: true}
	if e := transform.Check(target, "rating"); e != nil {
		t.Fatal(e)
	}
	value, evidence, e := transform.Apply(context.Background(), " 8.4分 ", target, false, "rating")
	if e != nil || JSON(value) != "8.4" || evidence.Before != " 8.4分 " || evidence.Hash != Hash(transform) {
		t.Fatal(value, evidence, e)
	}
	transform.Samples[0].Expected = json.RawMessage(`1`)
	if e = transform.Check(target, "rating"); AsError(e).Code != "TRANSFORM_SAMPLE_MISMATCH" {
		t.Fatal(e)
	}
}
func TestCOTransformRejectsUnsafeCode(t *testing.T) {
	for _, source := range []string{
		`(()=>{while(true){};return value})()`, `value.constructor("return process")()`, `value["constr"+"uctor"]`,
		`Function("return 1")()`, `eval("1")`, `fetch(value)`, `require("fs")`, `globalThis`, `Date.now()`, `Math.random()`,
		`value.repeat(999999)`, `value.match(/a+/)`, `value.map(x=>x)`, `value=1`, `new Array(999999)`, `({__proto__:value})`,
		`value["__proto__"]`, `value.__defineGetter__("a", value)`, `JSON.stringify(value)`, `value++`, `1n`, `value; 1`,
	} {
		if _, e := compileTransform(source); e == nil {
			t.Errorf("unsafe accepted: %s", source)
		}
	}
}
func TestCOTransformBudgetsNullAndIsolation(t *testing.T) {
	target := Field{Key: "v", Type: "string", Required: true}
	tr := transformFixture(`value+value`, `"x"`, `"xx"`)
	_, _, e := tr.Apply(context.Background(), strings.Repeat("x", 5000), target, false, "field")
	if e == nil {
		t.Fatal("large intermediate accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e = tr.Apply(ctx, "x", target, false, "field")
	if AsError(e).Code != "TRANSFORM_CANCELLED" {
		t.Fatal(e)
	}
	tr = transformFixture(`value === "" ? null : value`, `""`, `null`)
	tr.Input.EmptyAllowed = true
	target.Nullable = true
	if e = tr.Check(target, "field"); e != nil {
		t.Fatal(e)
	}
	target.Nullable = false
	if e = tr.Check(target, "field"); e == nil {
		t.Fatal("nullable violation")
	}
	tr = transformFixture(`value.split(",")`, `"a,b"`, `["a","b"]`)
	target.Multiple = true
	target.Type = "string"
	if e = tr.Check(target, "field"); e != nil {
		t.Fatal(e)
	}
	_, _, e = tr.Apply(context.Background(), strings.Repeat("a,", 101), target, false, "field")
	if e == nil {
		t.Fatal("array budget")
	}
	tr = transformFixture(`({n:value.n + 1})`, `{"n":1}`, `{"n":2}`)
	tr.Input.Type = "json"
	target.Type = "json"
	target.Multiple = false
	input := map[string]any{"n": 1}
	v, _, e := tr.Apply(context.Background(), input, target, false, "field")
	if e != nil || JSON(v) != `{"n":2}` || input["n"] != 1 {
		t.Fatal(v, input, e)
	}
}
func TestCOTransformExtractEvidence(t *testing.T) {
	d := baseline(t).Collectors[1].Definition
	d.Fetch.Kind = "http_html"
	for i := range d.Output.Mapping {
		if d.Output.Mapping[i].Key == "rating" {
			d.Output.Mapping[i].Transform = transformFixture(`Number(value.replace("分", ""))`, `"9.2分"`, `9.2`)
		}
	}
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	doc := Document{URL: "https://fixture.example.test/subject/m001", Format: "html", Content: `<main><h1>Title</h1><span class="rating">9.2分</span><a class="comments" href="/comments">comments</a></main>`}
	input := map[string]any{"movie_id": "m001", "url": doc.URL}
	p, e := Extract(context.Background(), d, input, doc, 10)
	if e != nil || len(p.Candidates) != 1 || !p.Candidates[0].Valid || p.Candidates[0].Fields["rating"].Transform == nil {
		t.Fatal(p, e)
	}
	doc.Content = strings.ReplaceAll(doc.Content, "9.2分", "unknown")
	p, e = Extract(context.Background(), d, input, doc, 10)
	if e != nil || p.Candidates[0].Valid || p.Candidates[0].Fields["rating"].Error == nil {
		t.Fatal(p, e)
	}
	// Arrays are collected by the raw Schema even if the result is scalar.
	for i := range d.Output.Mapping {
		if d.Output.Mapping[i].Key == "rating" {
			tr := transformFixture(`Number(value[0])+Number(value[1])`, `["1","2"]`, `3`)
			tr.Input.Multiple = true
			d.Output.Mapping[i].Transform = tr
		}
	}
	doc.Content = strings.ReplaceAll(doc.Content, `<span class="rating">unknown</span>`, `<span class="rating">1</span><span class="rating">2</span>`)
	p, e = Extract(context.Background(), d, input, doc, 10)
	if e != nil || !p.Candidates[0].Valid || JSON(p.Candidates[0].Values["rating"]) != "3" {
		t.Fatal(p, e)
	}
}
