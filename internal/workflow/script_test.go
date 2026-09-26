package workflow

import (
	"context"
	"testing"
)

func scriptNode(code string) Node {
	return Node{Name: "js_clean", Type: "script", Config: map[string]any{"script": code, "timeout_ms": float64(50), "input_fields": []any{"url", "title", "body"}, "output_fields": []any{"url", "title", "body"}}}
}

func TestRecordScriptSharedExecutionAndContract(t *testing.T) {
	d := Templates()[0].Definition
	d.Nodes = append(d.Nodes, scriptNode(`return {...input, title: input.title.toUpperCase()};`))
	doc := FetchResult{HTML: `<link rel="canonical" href="https://example.org/js"><h1>Title</h1><article>Body</article>`}
	values, steps, err := TraceRecordCandidates(d, doc, "trigger", articleSchema())
	if err != nil || len(values) != 1 || values[0]["title"] != "TITLE" || steps[len(steps)-1].Type != "script" {
		t.Fatalf("JS trace: %+v %+v %v", values, steps, err)
	}
	d.Nodes[len(d.Nodes)-1].Config["script"] = `return {title: 'missing key'};`
	if _, _, err := TraceRecordCandidates(d, doc, "trigger", articleSchema()); err == nil {
		t.Fatal("invalid script output accepted")
	}
	d.Nodes[len(d.Nodes)-1].Config["input_fields"] = []any{"unknown"}
	if err := d.ValidateRecordSchema(articleSchema(), "trigger"); err == nil {
		t.Fatal("unknown script input accepted")
	}
}

func TestRecordScriptTimeoutAndInputIsolation(t *testing.T) {
	input := map[string]any{"url": "https://example.org/a", "title": "original", "body": "Body"}
	node := scriptNode(`input.title = 'changed'; return input;`)
	if _, err := ExecuteRecordScript(context.Background(), node.Config, input); err != nil {
		t.Fatal(err)
	}
	if input["title"] != "original" {
		t.Fatal("script mutated caller input")
	}
	node.Config["script"] = `while(true) {}`
	if _, err := ExecuteRecordScript(context.Background(), node.Config, input); err == nil {
		t.Fatal("infinite script was not interrupted")
	}
}

func TestRecordScriptPublicationRejectsInvalidCodeAndMissingKey(t *testing.T) {
	d := Templates()[0].Definition
	node := scriptNode(`return {;`)
	d.Nodes = append(d.Nodes, node)
	if err := d.ValidateExecutable(); err == nil {
		t.Fatal("invalid JS syntax accepted")
	}
	node = scriptNode(`return {title: input.title};`)
	node.Config["output_fields"] = []any{"title"}
	d.Nodes[len(d.Nodes)-1] = node
	if err := d.ValidateRecordSchema(articleSchema(), "trigger"); err == nil {
		t.Fatal("script declaration removed required key at publication")
	}
}
