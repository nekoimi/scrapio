package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/config"
)

func TestAIStructuredBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		tokens        int
		want          bool
	}{
		{"valid", `{"explanation":"有界解释"}`, 10, true},
		{"invalid_json", `not json`, 10, false},
		{"token_budget", `{}`, 1001, false},
		{"response_budget", strings.Repeat("x", MaxAssistOutput+1), 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p map[string]any
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
				}
				if p["max_tokens"] != float64(1000) {
					t.Error("missing token limit")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": tc.content}}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": tc.tokens}})
			}))
			defer s.Close()
			r, e := (AssistClient{}).Structured(context.Background(), &config.AIAssistConfig{Enabled: true, BaseURL: s.URL, Model: "test"}, "untrusted sample", json.RawMessage(`{}`))
			if (e == nil) != tc.want {
				t.Fatal(r, e)
			}
		})
	}
	if _, e := (AssistClient{}).Structured(context.Background(), &config.AIAssistConfig{Enabled: true, BaseURL: "http://remote.example.test", Model: "test"}, "", json.RawMessage(`{}`)); e == nil {
		t.Fatal("non-local HTTP accepted")
	}
	if _, e := (AssistClient{}).Structured(context.Background(), &config.AIAssistConfig{Enabled: true, BaseURL: "http://127.0.0.1", Model: "test"}, "", json.RawMessage(`broken`)); e == nil {
		t.Fatal("invalid input accepted")
	}
}

func TestAIStructuredNoRedirect(t *testing.T) {
	called := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
	defer target.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer gateway.Close()
	_, e := (AssistClient{HTTP: gateway.Client()}).Structured(context.Background(), &config.AIAssistConfig{Enabled: true, BaseURL: gateway.URL, Model: "test"}, "", json.RawMessage(`{}`))
	if e == nil || called != 0 {
		t.Fatal("redirect followed", e, called)
	}
}
