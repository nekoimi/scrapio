// Package field defines the pure conversion/validation contract shared by
// v2.2 extraction, previews and future trial/run execution. It performs no I/O.
package field

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Key        string `json:"field_key,omitempty"`
	Type       string `json:"type,omitempty"`
	Required   bool   `json:"required,omitempty"`
	Multiple   bool   `json:"multiple,omitempty"`
	Clean      string `json:"clean,omitempty"`
	Regex      string `json:"regex,omitempty"`
	DateFormat string `json:"date_format,omitempty"`
}

func (o Options) Validate() error {
	if o.Key != "" && !regexp.MustCompile(`^[\p{L}\p{N}_-]{1,128}$`).MatchString(o.Key) {
		return errors.New("invalid field_key")
	}
	switch o.Type {
	case "", "string", "integer", "number", "boolean", "date", "datetime", "json":
	default:
		return errors.New("unsupported field type")
	}
	if o.Clean != "" && o.Clean != "trim" && o.Clean != "whitespace" {
		return errors.New("unsupported field cleaning")
	}
	if len(o.Regex) > 512 || len(o.DateFormat) > 64 {
		return errors.New("field processing exceeds budget")
	}
	if o.Regex != "" {
		if _, err := regexp.Compile(o.Regex); err != nil {
			return errors.New("invalid field regex")
		}
	}
	if o.DateFormat != "" && o.Type != "date" && o.Type != "datetime" {
		return errors.New("date_format requires date/datetime type")
	}
	return nil
}

func Empty(v any) bool {
	if v == nil {
		return true
	}
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value) == ""
	case []any:
		return len(value) == 0
	}
	return false
}

// Convert applies cleaning, an optional RE2 match/first group, and strict type
// conversion to one value. Multiple values are processed by the caller.
func Convert(v any, o Options) (any, string) {
	if v == nil {
		return nil, ""
	}
	if text, ok := v.(string); ok {
		switch o.Clean {
		case "trim":
			text = strings.TrimSpace(text)
		case "whitespace":
			text = strings.Join(strings.Fields(text), " ")
		}
		if o.Regex != "" {
			expression, err := regexp.Compile(o.Regex)
			if err != nil {
				return nil, "INVALID_REGEX"
			}
			matches := expression.FindStringSubmatch(text)
			if len(matches) == 0 {
				return nil, "REGEX_NO_MATCH"
			}
			text = matches[0]
			if len(matches) > 1 {
				text = matches[1]
			}
		}
		v = text
	} else if o.Regex != "" || o.Clean != "" {
		return nil, "EXPECTED_TEXT"
	}
	if Empty(v) {
		return nil, ""
	}
	if o.Type == "" || o.Type == "json" {
		return v, ""
	}
	var text string
	switch value := v.(type) {
	case string:
		text = value
	case json.Number:
		text = value.String()
	case bool:
		text = strconv.FormatBool(value)
	default:
		return nil, "TYPE_MISMATCH"
	}
	text = strings.TrimSpace(text)
	switch o.Type {
	case "string":
		switch value := v.(type) {
		case string:
			return value, ""
		default:
			return text, ""
		}
	case "integer":
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, "INVALID_INTEGER"
		}
		return json.Number(strconv.FormatInt(number, 10)), ""
	case "number":
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, "INVALID_NUMBER"
		}
		if !json.Valid([]byte(text)) {
			return nil, "INVALID_NUMBER"
		}
		var raw any
		d := json.NewDecoder(strings.NewReader(text))
		d.UseNumber()
		if d.Decode(&raw) != nil {
			return nil, "INVALID_NUMBER"
		}
		if _, ok := raw.(json.Number); !ok {
			return nil, "INVALID_NUMBER"
		}
		return raw, ""
	case "boolean":
		if text == "true" {
			return true, ""
		}
		if text == "false" {
			return false, ""
		}
		return nil, "INVALID_BOOLEAN"
	case "date", "datetime":
		layout := o.DateFormat
		if layout == "" {
			layout = "2006-01-02"
			if o.Type == "datetime" {
				layout = time.RFC3339
			}
		}
		parsed, err := time.Parse(layout, text)
		if err != nil {
			return nil, "INVALID_DATE"
		}
		if o.Type == "date" {
			return parsed.Format("2006-01-02"), ""
		}
		return parsed.UTC().Format(time.RFC3339), ""
	}
	return nil, "TYPE_MISMATCH"
}
