package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Request struct {
	Model    string
	Prompt   string
	Document string
	Schema   map[string]any
}

type Response struct {
	Model        string
	Content      string
	InputTokens  int
	OutputTokens int
}

type Provider interface {
	Extract(Request) (Response, error)
}

type Result struct {
	Values       map[string]any `json:"values"`
	Confidence   float64        `json:"confidence,omitempty"`
	ReviewStatus string         `json:"review_status"`
	Model        string         `json:"model"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
}

func CacheKey(request Request) string {
	data, _ := json.Marshal(request)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func ParseResponse(response Response, schema map[string]any) (Result, error) {
	if strings.TrimSpace(response.Content) == "" {
		return Result{}, errors.New("AI response is empty")
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(response.Content), &values); err != nil {
		return Result{}, fmt.Errorf("AI response is not JSON: %w", err)
	}
	if values == nil {
		return Result{}, errors.New("AI response must be a JSON object")
	}
	if err := ValidateSchema(values, schema); err != nil {
		return Result{}, err
	}
	confidence := 0.0
	if raw, ok := values["confidence"]; ok {
		if number, ok := raw.(float64); ok {
			confidence = number
			delete(values, "confidence")
		}
	}
	return Result{Values: values, Confidence: confidence, ReviewStatus: "pending_review", Model: response.Model, InputTokens: response.InputTokens, OutputTokens: response.OutputTokens}, nil
}

func ValidateSchema(values map[string]any, schema map[string]any) error {
	if schema == nil {
		return nil
	}
	required, _ := schema["required"].([]any)
	for _, raw := range required {
		name := fmt.Sprint(raw)
		if value, ok := values[name]; !ok || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return fmt.Errorf("required AI field %q is empty", name)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for name, raw := range properties {
		property, _ := raw.(map[string]any)
		expected := strings.ToLower(fmt.Sprint(property["type"]))
		if value, ok := values[name]; ok && expected != "" && !matchesType(value, expected) {
			return fmt.Errorf("AI field %q has invalid type", name)
		}
	}
	return nil
}

func matchesType(value any, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "number", "integer":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return true
	}
}
