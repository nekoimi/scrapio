// Package regression replays frozen documents. It never navigates or writes records.
package regression

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/sample"
)

const MaxBatchBytes = 16 * 1024 * 1024
const MaxReportBytes = 4 * 1024 * 1024

type Input struct {
	ExpectedRevision int    `json:"expected_revision"`
	BaseVersionID    string `json:"base_version_id,omitempty"`
	TargetVersionID  string `json:"target_version_id,omitempty"`
}
type Document struct {
	ID           string          `json:"sample_id"`
	Name         string          `json:"name"`
	Revision     int             `json:"revision"`
	Kind         string          `json:"kind"`
	StepID       string          `json:"step_id"`
	Stage        string          `json:"stage"`
	Content      string          `json:"content"`
	ContentHash  string          `json:"content_hash"`
	Expected     json.RawMessage `json:"expected"`
	ExpectedHash string          `json:"expected_hash"`
	Format       string          `json:"format"`
	URL          string          `json:"url"`
	BaseURL      string          `json:"base_url"`
	Error        string          `json:"error_code,omitempty"`
}
type Snapshot struct {
	Base    json.RawMessage `json:"base,omitempty"`
	Target  json.RawMessage `json:"target"`
	Samples []Document      `json:"samples"`
}
type Difference struct {
	Path          string `json:"path"`
	Before        string `json:"before_json"`
	After         string `json:"after_json"`
	BeforeHash    string `json:"before_hash"`
	AfterHash     string `json:"after_hash"`
	BeforePresent bool   `json:"before_present"`
	AfterPresent  bool   `json:"after_present"`
}
type Diff struct {
	Items     []Difference `json:"items"`
	Truncated bool         `json:"truncated"`
}
type Evaluation struct {
	Status       string               `json:"status"`
	ErrorCode    string               `json:"error_code,omitempty"`
	RecordCount  int                  `json:"record_count"`
	ValidCount   int                  `json:"valid_count"`
	InvalidCount int                  `json:"invalid_count"`
	Comparison   *AssertionComparison `json:"comparison,omitempty"`
}
type AssertionDifference struct {
	sample.Difference
	ExpectedHash string `json:"expected_hash"`
	ActualHash   string `json:"actual_hash"`
	Clipped      bool   `json:"clipped"`
}
type AssertionComparison struct {
	Status         string                `json:"status"`
	AssertionCount int                   `json:"assertion_count"`
	Differences    []AssertionDifference `json:"differences"`
}

func snippet(raw string) string {
	if len(raw) <= 512 {
		return raw
	}
	r := []rune(raw)
	return string(r[:min(128, len(r))]) + "…"
}

type SampleResult struct {
	ID           string      `json:"sample_id"`
	Name         string      `json:"name"`
	Revision     int         `json:"revision"`
	StepID       string      `json:"step_id"`
	Stage        string      `json:"stage"`
	Kind         string      `json:"kind"`
	ContentHash  string      `json:"content_hash"`
	ExpectedHash string      `json:"expected_hash"`
	Target       Evaluation  `json:"target"`
	Base         *Evaluation `json:"base,omitempty"`
	Records      *Diff       `json:"records,omitempty"`
}
type Report struct {
	Verdict         string         `json:"verdict"`
	Definition      *Diff          `json:"definition,omitempty"`
	Samples         []SampleResult `json:"samples"`
	Interpreter     string         `json:"interpreter_version"`
	Alignment       string         `json:"alignment"`
	NetworkAccessed bool           `json:"network_accessed"`
	DryRun          bool           `json:"dry_run"`
}

func decode(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}
func equal(a, b any) bool {
	x, xok := a.(json.Number)
	y, yok := b.(json.Number)
	if xok && yok {
		u, ok := new(big.Rat).SetString(string(x))
		v, good := new(big.Rat).SetString(string(y))
		return ok && good && u.Cmp(v) == 0
	}
	ma, oka := a.(map[string]any)
	mb, okb := b.(map[string]any)
	if oka && okb {
		if len(ma) != len(mb) {
			return false
		}
		for k, v := range ma {
			w, found := mb[k]
			if !found || !equal(v, w) {
				return false
			}
		}
		return true
	}
	aa, oka := a.([]any)
	ab, okb := b.([]any)
	if oka && okb {
		if len(aa) != len(ab) {
			return false
		}
		for i, v := range aa {
			if !equal(v, ab[i]) {
				return false
			}
		}
		return true
	}
	xr, _ := json.Marshal(a)
	yr, _ := json.Marshal(b)
	return bytes.Equal(xr, yr)
}
func cell(v any, present bool) (string, string) {
	if !present {
		return "", ""
	}
	raw, _ := json.Marshal(v)
	hash := capture.Hash(raw)
	if len(raw) > 512 {
		return string([]rune(string(raw))[:min(128, len([]rune(string(raw))))]) + "…", hash
	}
	return string(raw), hash
}

// CompareJSON uses JSON pointers. Missing values differ from explicit null.
func CompareJSON(a, b []byte) (Diff, error) {
	x, err := decode(a)
	if err != nil {
		return Diff{}, err
	}
	y, err := decode(b)
	if err != nil {
		return Diff{}, err
	}
	out := Diff{Items: []Difference{}}
	var walk func(string, any, any, bool, bool)
	walk = func(path string, a, b any, ap, bp bool) {
		if ap == bp && equal(a, b) {
			return
		}
		if len(out.Items) >= 200 {
			out.Truncated = true
			return
		}
		ma, oka := a.(map[string]any)
		mb, okb := b.(map[string]any)
		if ap && bp && oka && okb {
			keys := map[string]bool{}
			for k := range ma {
				keys[k] = true
			}
			for k := range mb {
				keys[k] = true
			}
			list := []string{}
			for k := range keys {
				list = append(list, k)
			}
			sort.Strings(list)
			for _, k := range list {
				v, p := ma[k]
				w, q := mb[k]
				walk(path+"/"+strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"), v, w, p, q)
			}
			return
		}
		aa, oka := a.([]any)
		ab, okb := b.([]any)
		if ap && bp && oka && okb {
			for i := 0; i < max(len(aa), len(ab)); i++ {
				var v, w any
				p, q := i < len(aa), i < len(ab)
				if p {
					v = aa[i]
				}
				if q {
					w = ab[i]
				}
				walk(path+"/"+strconv.Itoa(i), v, w, p, q)
			}
			return
		}
		v, h := cell(a, ap)
		w, j := cell(b, bp)
		out.Items = append(out.Items, Difference{path, v, w, h, j, ap, bp})
	}
	walk("", x, y, true, true)
	return out, nil
}
func evaluate(ctx context.Context, def []byte, d Document) (Evaluation, []byte) {
	out := Evaluation{Status: "incomplete"}
	if d.Error != "" {
		out.ErrorCode = d.Error
		return out, nil
	}
	if d.Content == "" || capture.Hash([]byte(d.Content)) != d.ContentHash {
		out.ErrorCode = "INPUT_UNAVAILABLE"
		return out, nil
	}
	p, err := extraction.PlanFromDefinition(def, d.StepID)
	if err != nil {
		out.Status = "failed"
		out.ErrorCode = "STEP_INCOMPATIBLE"
		return out, nil
	}
	r, err := extraction.Extract(ctx, extraction.Input{Content: d.Content, Format: d.Format, URL: d.URL, BaseURL: d.BaseURL, Stage: d.Stage}, p)
	if err != nil {
		out.Status = "failed"
		out.ErrorCode = "EXTRACTION_FAILED"
		return out, nil
	}
	e, err := sample.DecodeExpected(d.Expected)
	if err != nil || capture.Hash(sample.CanonicalJSON(d.Expected)) != d.ExpectedHash {
		out.ErrorCode = "EXPECTATION_UNAVAILABLE"
		return out, nil
	}
	c := sample.Compare(r, e)
	out.Status = c.Status
	out.RecordCount = r.MatchCount
	out.ValidCount = r.ValidCount
	out.InvalidCount = r.InvalidCount
	comparison := AssertionComparison{Status: c.Status, AssertionCount: c.AssertionCount, Differences: []AssertionDifference{}}
	for _, d := range c.Differences {
		fullExpected, fullActual := d.Expected, d.Actual
		d.Expected = snippet(fullExpected)
		d.Actual = snippet(fullActual)
		comparison.Differences = append(comparison.Differences, AssertionDifference{Difference: d, ExpectedHash: capture.Hash([]byte(fullExpected)), ActualHash: capture.Hash([]byte(fullActual)), Clipped: d.Expected != fullExpected || d.Actual != fullActual})
	}
	out.Comparison = &comparison
	if r.Truncated || r.MatchCountLowerBound {
		out.Status = "incomplete"
		out.ErrorCode = "EXTRACTION_TRUNCATED"
	}
	// Only semantic fields and record validity are compared; diagnostics like locators are definition differences.
	rows := []map[string]any{}
	for _, record := range r.Records {
		fields := map[string]any{}
		for _, f := range record.Fields {
			if f.Truncated {
				out.Status = "incomplete"
				out.ErrorCode = "FIELD_TRUNCATED"
			}
			value, _ := decode([]byte(f.ValueJSON))
			fields[f.Key] = map[string]any{"value": value, "errors": f.Errors, "valid": f.Valid}
		}
		rows = append(rows, map[string]any{"valid": record.Valid, "fields": fields})
	}
	raw, _ := json.Marshal(map[string]any{"record_count": r.MatchCount, "valid_count": r.ValidCount, "invalid_count": r.InvalidCount, "records": rows})
	return out, raw
}
func Execute(ctx context.Context, s Snapshot, progress func(int) error) (Report, error) {
	out := Report{Verdict: "unconfigured", Samples: []SampleResult{}, Interpreter: extraction.InterpreterVersion, Alignment: "record_index", DryRun: true}
	if len(s.Base) > 0 {
		diff, err := CompareJSON(s.Base, s.Target)
		if err != nil {
			return out, err
		}
		out.Definition = &diff
	}
	passed, failed, incomplete, unconfigured := 0, 0, 0, 0
	for i, d := range s.Samples {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		t, rows := evaluate(ctx, s.Target, d)
		r := SampleResult{ID: d.ID, Name: d.Name, Revision: d.Revision, StepID: d.StepID, Stage: d.Stage, Kind: d.Kind, ContentHash: d.ContentHash, ExpectedHash: d.ExpectedHash, Target: t}
		if len(s.Base) > 0 {
			b, old := evaluate(ctx, s.Base, d)
			r.Base = &b
			if old != nil && rows != nil {
				diff, err := CompareJSON(old, rows)
				if err != nil {
					return out, err
				}
				r.Records = &diff
			}
		}
		switch t.Status {
		case "passed":
			passed++
		case "failed":
			failed++
		case "incomplete":
			incomplete++
		default:
			unconfigured++
		}
		out.Samples = append(out.Samples, r)
		if err := progress(i + 1); err != nil {
			return out, err
		}
	}
	if failed > 0 {
		out.Verdict = "failed"
	} else if incomplete > 0 {
		out.Verdict = "incomplete"
	} else if unconfigured > 0 || passed == 0 {
		out.Verdict = "unconfigured"
	} else {
		out.Verdict = "passed"
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > MaxReportBytes {
		return Report{}, errors.New("REPORT_BUDGET_EXCEEDED")
	}
	return out, nil
}
