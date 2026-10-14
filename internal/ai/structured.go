package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/config"
)

type ResponseUnavailableError struct{}

func (ResponseUnavailableError) Error() string {
	return "AI gateway response unavailable; do not repeat the original request"
}
func IsResponseUnknown(err error) bool {
	var unavailable ResponseUnavailableError
	return errors.As(err, &unavailable)
}

// Structured is a transport only. The independent control plane checks the
// untrusted response using its actual interpreter, never the legacy extractor.
func (c AssistClient) Structured(ctx context.Context, cfg *config.AIAssistConfig, system string, input json.RawMessage) (Response, error) {
	if cfg == nil || !cfg.Enabled || strings.TrimSpace(cfg.Model) == "" {
		return Response{}, errors.New("AI gateway is disabled or incomplete")
	}
	endpoint, e := url.Parse(cfg.BaseURL)
	if e != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (endpoint.Hostname() == "localhost" || endpoint.Hostname() == "127.0.0.1"))) {
		return Response{}, errors.New("invalid AI gateway configuration")
	}
	if len(input) > 24576 || !json.Valid(input) {
		return Response{}, errors.New("AI input budget exceeded")
	}
	payload, e := json.Marshal(map[string]any{"model": cfg.Model, "max_tokens": 1000, "temperature": 0, "response_format": map[string]string{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": string(input)}}})
	if e != nil {
		return Response{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if e != nil {
		return Response{}, errors.New("invalid AI request")
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.HTTP != nil {
		client.Transport = c.HTTP.Transport
	}
	resp, e := client.Do(req)
	if e != nil {
		return Response{}, ResponseUnavailableError{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, errors.New("AI gateway refused the request")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, MaxAssistOutput+1))
	if e != nil {
		return Response{}, ResponseUnavailableError{}
	}
	if len(data) > MaxAssistOutput {
		return Response{}, errors.New("AI response over budget")
	}
	var envelope struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		}
	}
	if e = json.Unmarshal(data, &envelope); e != nil || len(envelope.Choices) != 1 || envelope.Choices[0].FinishReason == "length" {
		return Response{}, errors.New("AI response is malformed or truncated")
	}
	r := Response{Model: envelope.Model, Content: envelope.Choices[0].Message.Content, InputTokens: -1, OutputTokens: -1}
	if r.Model == "" {
		r.Model = cfg.Model
	}
	if envelope.Usage != nil {
		r.InputTokens = envelope.Usage.PromptTokens
		r.OutputTokens = envelope.Usage.CompletionTokens
		if r.InputTokens < 0 || r.OutputTokens < 0 || r.OutputTokens > 1000 {
			return Response{}, errors.New("AI usage exceeded budget")
		}
	}
	if len(r.Content) > 32768 || !json.Valid([]byte(r.Content)) || len(r.Model) > 256 {
		return Response{}, errors.New("AI result is not bounded JSON")
	}
	return r, nil
}
