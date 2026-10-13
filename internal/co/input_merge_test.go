package co

import "testing"

func TestCOInputMergeIdentity(t *testing.T) {
	a := TaskStage{CollectorVersionID: "v1", InputPolicy: "merge_identical"}
	input := `{"url":"https://fixture.example.test/a","language":"zh"}`
	reordered := `{"language":"zh", "url":"https://fixture.example.test/a"}`
	if mergeKey(a, input) != mergeKey(a, reordered) || mergeKey(a, input) == "" {
		t.Fatal("JSON order must not change normalized identity")
	}
	b := a
	b.CollectorVersionID = "v2"
	if mergeKey(a, input) == mergeKey(b, input) || mergeKey(a, input) == mergeKey(a, `{"url":"https://fixture.example.test/a","language":"en"}`) {
		t.Fatal("different versions or non-URL parameters must not merge")
	}
	a.InputPolicy = ""
	if mergeKey(a, input) != "" {
		t.Fatal("old manifests must keep independent sources")
	}
	a.InputPolicy = "keep_all"
	if mergeKey(a, input) != "" {
		t.Fatal("explicit keep_all must not merge")
	}
}
