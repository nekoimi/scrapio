// Package sample evaluates bounded, explicit expectations against the same
// fixed-document interpreter used by preview and future runs. It performs no I/O.
package sample

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strings"

	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/field"
)

type Expected struct {
	RecordCount  *int            `json:"record_count,omitempty"`
	ValidCount   *int            `json:"valid_count,omitempty"`
	InvalidCount *int            `json:"invalid_count,omitempty"`
	Fields       []FieldExpected `json:"fields,omitempty"`
}
type FieldExpected struct {
	RecordIndex int             `json:"record_index"`
	FieldKey    string          `json:"field_key"`
	Value       json.RawMessage `json:"value,omitempty"`
	Errors      *[]string       `json:"errors,omitempty"`
}
type Difference struct {
	Code        string `json:"code"`
	RecordIndex *int   `json:"record_index,omitempty"`
	FieldKey    string `json:"field_key,omitempty"`
	Expected    string `json:"expected_json,omitempty"`
	Actual      string `json:"actual_json,omitempty"`
}
type Comparison struct {
	Status         string       `json:"status"`
	AssertionCount int          `json:"assertion_count"`
	Differences    []Difference `json:"differences"`
}

func DecodeExpected(data []byte) (Expected, error) {
	var out Expected
	if len(data) == 0 || len(data) > 64*1024 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return out, errors.New("expected must be a bounded object")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return out, errors.New("unsupported expected configuration")
	}
	if d.Decode(new(any)) != io.EOF {
		return out, errors.New("expected must contain one object")
	}
	var shape struct {
		Fields []map[string]json.RawMessage `json:"fields"`
	}
	_ = json.Unmarshal(data, &shape)
	for _, f := range shape.Fields {
		if raw, has := f["record_index"]; !has || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return out, errors.New("explicit record_index is required")
		}
	}
	for _, n := range []*int{out.RecordCount, out.ValidCount, out.InvalidCount} {
		if n != nil && (*n < 0 || *n > 1000000) {
			return out, errors.New("expected counts must be between 0 and 1000000")
		}
	}
	if len(out.Fields) > 100 {
		return out, errors.New("at most 100 field assertions")
	}
	seen := map[string]map[int]bool{}
	for _, f := range out.Fields {
		if f.RecordIndex < 0 || f.RecordIndex > 19 || f.FieldKey == "" || (len(f.Value) == 0 && f.Errors == nil) {
			return out, errors.New("field assertion requires record_index (0..19), field_key and value/errors")
		}
		if err := (field.Options{Key: f.FieldKey}).Validate(); err != nil {
			return out, err
		}
		if seen[f.FieldKey] == nil {
			seen[f.FieldKey] = map[int]bool{}
		}
		if seen[f.FieldKey][f.RecordIndex] {
			return out, errors.New("duplicate field assertion")
		}
		seen[f.FieldKey][f.RecordIndex] = true
		if len(f.Value) > 8192 || len(f.Value) > 0 && !json.Valid(f.Value) {
			return out, errors.New("expected value exceeds budget or is invalid JSON")
		}
		if f.Errors != nil {
			if len(*f.Errors) > 20 {
				return out, errors.New("too many expected errors")
			}
			codes := map[string]bool{}
			for _, code := range *f.Errors {
				if code == "" || len(code) > 64 || codes[code] {
					return out, errors.New("invalid/duplicate expected error")
				}
				codes[code] = true
			}
		}
	}
	return out, nil
}

func Compare(result extraction.Result, expected Expected) Comparison {
	out := Comparison{Status: "unconfigured", Differences: []Difference{}}
	add := func(code string, index *int, key string, want, got any) {
		w, _ := json.Marshal(want)
		g, _ := json.Marshal(got)
		out.Differences = append(out.Differences, Difference{Code: code, RecordIndex: index, FieldKey: key, Expected: string(w), Actual: string(g)})
	}
	for _, n := range []struct {
		p      *int
		actual int
		code   string
	}{{expected.RecordCount, result.MatchCount, "RECORD_COUNT_MISMATCH"}, {expected.ValidCount, result.ValidCount, "VALID_COUNT_MISMATCH"}, {expected.InvalidCount, result.InvalidCount, "INVALID_COUNT_MISMATCH"}} {
		if n.p != nil {
			out.AssertionCount++
			if n.code == "RECORD_COUNT_MISMATCH" && result.MatchCountLowerBound {
				add("COUNT_INCOMPLETE", nil, "", *n.p, n.actual)
			} else if *n.p != n.actual {
				add(n.code, nil, "", *n.p, n.actual)
			}
		}
	}
	expectedErrors := map[int]map[string][]string{}
	for _, want := range expected.Fields {
		index := want.RecordIndex
		if len(want.Value) > 0 {
			out.AssertionCount++
		}
		if want.Errors != nil {
			out.AssertionCount++
		}
		if index >= len(result.Records) {
			add("RECORD_NOT_PREVIEWED", &index, want.FieldKey, nil, nil)
			continue
		}
		var got *extraction.FieldResult
		for j := range result.Records[index].Fields {
			f := &result.Records[index].Fields[j]
			if f.Key == want.FieldKey {
				got = f
				break
			}
		}
		if got == nil {
			add("FIELD_NOT_FOUND", &index, want.FieldKey, nil, nil)
			continue
		}
		if len(want.Value) > 0 && !equalJSON(want.Value, []byte(got.ValueJSON)) {
			add("VALUE_MISMATCH", &index, want.FieldKey, json.RawMessage(want.Value), json.RawMessage(got.ValueJSON))
		}
		if want.Errors != nil {
			if expectedErrors[index] == nil {
				expectedErrors[index] = map[string][]string{}
			}
			expectedErrors[index][want.FieldKey] = *want.Errors
			if !sameCodes(*want.Errors, got.Errors) {
				add("ERRORS_MISMATCH", &index, want.FieldKey, *want.Errors, got.Errors)
			}
		}
	}
	if out.AssertionCount == 0 {
		return out
	}
	// Negative samples may explicitly expect field errors. Other invalid fields
	// remain failures, even when unrelated count/value assertions happen to pass.
	for _, record := range result.Records {
		index := record.Index
		if len(record.Fields) == 0 {
			add("NO_FIELDS", &index, "", nil, nil)
		}
		for _, f := range record.Fields {
			if !f.Valid {
				want, has := expectedErrors[index][f.Key]
				if !has || !sameCodes(want, f.Errors) {
					add("UNEXPECTED_FIELD_ERROR", &index, f.Key, []string{}, f.Errors)
				}
			}
		}
	}
	if result.Truncated {
		add("PREVIEW_INCOMPLETE", nil, "", false, true)
	}
	out.Status = "passed"
	if len(out.Differences) > 0 {
		out.Status = "failed"
	}
	return out
}
func sameCodes(a, b []string) bool {
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}
func equalJSON(a, b []byte) bool {
	decode := func(raw []byte) any {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		_ = d.Decode(&v)
		return v
	}
	return equalValue(decode(a), decode(b))
}
func equalValue(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		return normalizedNumber(x.String()) == normalizedNumber(y.String())
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			other, has := y[k]
			if !has || !equalValue(v, other) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// Normalize decimal digits and exponent without constructing 10^exponent.
// JSON accepts arbitrarily large exponents; expanding them would be unbounded.
func normalizedNumber(raw string) string {
	sign := ""
	if strings.HasPrefix(raw, "-") {
		sign = "-"
		raw = raw[1:]
	}
	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(raw), "e")
	exponent := new(big.Int)
	if hasExponent {
		_, _ = exponent.SetString(exponentText, 10)
	}
	whole, fraction, hasFraction := strings.Cut(mantissa, ".")
	if hasFraction {
		exponent.Sub(exponent, big.NewInt(int64(len(fraction))))
	}
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0"
	}
	trimmed := strings.TrimRight(digits, "0")
	exponent.Add(exponent, big.NewInt(int64(len(digits)-len(trimmed))))
	return sign + trimmed + "e" + exponent.String()
}

// CanonicalJSON preserves types and precision while normalizing decimal
// notation. PostgreSQL JSONB expands exponents, so byte-only numeric hashing
// would incorrectly mark an unchanged saved expectation as corrupt.
func CanonicalJSON(raw []byte) []byte {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil {
		return nil
	}
	var normalize func(any) any
	normalize = func(v any) any {
		switch x := v.(type) {
		case json.Number:
			return json.Number(normalizedNumber(x.String()))
		case []any:
			for i := range x {
				x[i] = normalize(x[i])
			}
			return x
		case map[string]any:
			for k, v := range x {
				x[k] = normalize(v)
			}
			return x
		default:
			return v
		}
	}
	encoded, _ := json.Marshal(normalize(value))
	return encoded
}
