package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/config"
)

func TestAIAssistClientAndValidation(t *testing.T) {
	var seen AssistRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("unexpected request")
			w.WriteHeader(400)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 2 {
			t.Error("invalid completion payload")
			w.WriteHeader(400)
			return
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &seen); err != nil {
			t.Error(err)
		}
		response := map[string]any{"model": "fixture-model", "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 30}, "choices": []any{map[string]any{"message": map[string]any{"content": `{"suggestions":[{"field":"title","selector":"h1","reason":"heading"}],"explanation":"title selector changed"}`}}}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	input := AssistRequest{ContentType: "html", Content: `<script>token</script><h1>Title</h1><input value="secret">`, Fields: []RuleField{{Name: "title", Selector: ".old"}}}
	result, err := (AssistClient{}).Suggest(context.Background(), &config.AIAssistConfig{Enabled: true, BaseURL: server.URL + "/v1/chat/completions", APIKey: "fixture", Model: "fixture-model"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seen.Content, "token") || strings.Contains(seen.Content, "secret") || result.Model != "fixture-model" || result.OutputTokens != 30 {
		t.Fatalf("unsafe or invalid AI request/result: %+v %+v", seen, result)
	}
	trimmed, _ := TrimDocument(input.ContentType, input.Content)
	input.Content = trimmed
	if err := ValidateSuggestions(input, result); err != nil {
		t.Fatal(err)
	}
	result.Suggestions[0].Field = "invented"
	if err := ValidateSuggestions(input, result); err == nil {
		t.Fatal("invented field accepted")
	}
}

func TestAIAssistBounds(t *testing.T) {
	content, truncated := TrimDocument("json", strings.Repeat("中", MaxAssistInput))
	if !truncated || len(content) > MaxAssistInput || !json.Valid([]byte(`{"text":`+strconv.Quote(content)+`}`)) {
		t.Fatal("invalid clipped UTF-8")
	}
	_, err := (AssistClient{}).Suggest(context.Background(), &config.AIAssistConfig{Enabled: true, BaseURL: "http://example.org/v1/chat/completions", Model: "x"}, AssistRequest{Content: "a", Fields: []RuleField{{Name: "a"}}})
	if err == nil {
		t.Fatal("insecure remote gateway accepted")
	}
}

func TestValidateSuggestionsRequiresSampleMatch(t *testing.T) {
	for _, tc := range []struct {
		name, kind, content, selector string
	}{
		{"missing HTML node", "html", "<h1>Present</h1>", ".absent"},
		{"empty HTML text", "html", "<h1>  </h1>", "h1"},
		{"missing JSON property", "json", `{"title":"Present"}`, "$.absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := AssistRequest{ContentType: tc.kind, Content: tc.content, Fields: []RuleField{{Name: "title"}}}
			result := AssistResponse{Suggestions: []RuleSuggestion{{Field: "title", Selector: tc.selector}}}
			if err := ValidateSuggestions(input, result); err == nil {
				t.Fatal("selector with no sample value was accepted")
			}
		})
	}
}
