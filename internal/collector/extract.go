package collector

import (
	"context"
	"fmt"
	"github.com/andybalholm/cascadia"
	"github.com/antchfx/htmlquery"
	"github.com/antchfx/xpath"
	"github.com/nekoimi/scrapio/internal/capture"
	"golang.org/x/net/html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// JSONPath deliberately supports only $, dotted keys, indexes and [*]. No scripts/filters.
var pathToken = regexp.MustCompile(`^(?:\.([a-zA-Z_][a-zA-Z0-9_-]*)|\[([0-9]+|\*)\])`)

func jsonTokens(path string) ([]string, error) {
	if path == "" || path[0] != '$' {
		return nil, fmt.Errorf("JSONPath必须以$开头")
	}
	rest := path[1:]
	tokens := []string{}
	for rest != "" {
		m := pathToken.FindStringSubmatch(rest)
		if m == nil || len(tokens) >= 32 {
			return nil, fmt.Errorf("JSONPath只支持$.字段、[索引]、[*]，最多32级")
		}
		if m[1] != "" {
			tokens = append(tokens, "k:"+m[1])
		} else {
			tokens = append(tokens, m[2])
		}
		rest = rest[len(m[0]):]
	}
	return tokens, nil
}
func jsonSelect(value any, path string) ([]any, error) {
	tokens, e := jsonTokens(path)
	if e != nil {
		return nil, e
	}
	values := []any{value}
	for _, t := range tokens {
		next := []any{}
		for _, v := range values {
			if strings.HasPrefix(t, "k:") {
				if o, ok := v.(map[string]any); ok {
					if x, p := o[t[2:]]; p {
						next = append(next, x)
					}
				}
			} else if a, ok := v.([]any); ok {
				if t == "*" {
					next = append(next, a...)
				} else {
					i, _ := strconv.Atoi(t)
					if i < len(a) {
						next = append(next, a[i])
					}
				}
			}
			if len(next) > 10000 {
				return nil, fmt.Errorf("JSONPath命中超过10000条")
			}
		}
		values = next
	}
	return values, nil
}
func (l Locator) Validate(jsonSource bool) error {
	if len(l.Expression) < 1 || len(l.Expression) > 2048 {
		return fmt.Errorf("定位表达式需要1..2048字符")
	}
	if jsonSource {
		if l.Strategy != "jsonpath" {
			return fmt.Errorf("JSON使用JSONPath定位")
		}
		_, e := jsonTokens(l.Expression)
		return e
	}
	switch l.Strategy {
	case "css":
		_, e := cascadia.Compile(l.Expression)
		return e
	case "xpath":
		_, e := xpath.Compile(l.Expression)
		return e
	}
	return fmt.Errorf("HTML使用CSS或XPath定位")
}
func nodes(root *html.Node, l Locator) ([]*html.Node, error) {
	if l.Strategy == "css" {
		sel, e := cascadia.Compile(l.Expression)
		if e != nil {
			return nil, e
		}
		return cascadia.QueryAll(root, sel), nil
	}
	return htmlquery.QueryAll(root, l.Expression)
}

type Document struct {
	URL         string `json:"url"`
	Format      string `json:"format"`
	Content     string `json:"content"`
	PageStateID string `json:"page_state_id,omitempty"`
}
type FieldEvidence struct {
	MatchCount int                `json:"match_count"`
	Status     string             `json:"status"`
	Message    string             `json:"message,omitempty"`
	Error      *Error             `json:"error,omitempty"`
	Transform  *TransformEvidence `json:"transform,omitempty"`
}
type Candidate struct {
	Port     string                   `json:"port,omitempty"`
	Identity string                   `json:"identity,omitempty"`
	Values   map[string]any           `json:"values"`
	Valid    bool                     `json:"valid"`
	Fields   map[string]FieldEvidence `json:"fields"`
	Page     int                      `json:"page"`
}
type Page struct {
	Candidates    []Candidate
	Next          string
	EmptyVerified bool
	HasNext       bool
	Hash          string
	Truncated     bool
	PrimaryEmpty  bool
}

func Extract(ctx context.Context, d Definition, input map[string]any, doc Document, limit int) (Page, error) {
	if len(d.AdditionalOutputs) == 0 {
		return extractPort(ctx, d, input, doc, limit)
	}
	result := Page{}
	for i, port := range d.Ports() {
		one, _ := d.ForPort(port.Output.Port)
		p, e := extractPort(ctx, one, input, doc, limit-len(result.Candidates))
		if e != nil {
			return result, e
		}
		if i == 0 {
			result = p
			result.PrimaryEmpty = len(p.Candidates) == 0
			if len(p.Candidates) == 0 && !p.EmptyVerified {
				return result, Fault("ZERO_RESULT_UNVERIFIED", "records", "主端口缺少记录及空态证据，附加端口不能掩盖主规则失败")
			}
			result.Candidates = nil
		}
		for _, c := range p.Candidates {
			c.Port = port.Output.Port
			result.Candidates = append(result.Candidates, c)
		}
		result.Truncated = result.Truncated || p.Truncated
	}
	result.Hash = Hash(result.Candidates)
	return result, nil
}

func extractPort(ctx context.Context, d Definition, input map[string]any, doc Document, limit int) (Page, error) {
	result := Page{Hash: capture.Hash([]byte(doc.Content))}
	var root *html.Node
	var data any
	var records []any
	var e error
	if doc.Format == "json" {
		data, e = capture.DecodeJSON(doc.Content)
		if e == nil {
			records, e = jsonSelect(data, d.Records.Expression)
		}
	} else {
		root, e = html.Parse(strings.NewReader(doc.Content))
		if e == nil {
			var ns []*html.Node
			ns, e = nodes(root, d.Records)
			for _, n := range ns {
				records = append(records, n)
			}
		}
	}
	if e != nil {
		return result, Fault("INVALID_DOCUMENT", "records", e.Error())
	}
	if len(records) > 10000 {
		return result, Fault("RECORD_BUDGET", "records", "记录定位命中过多")
	}
	fields := map[string]Field{}
	for _, f := range d.Output.Fields {
		fields[f.Key] = f
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if len(result.Candidates) >= limit {
			result.Truncated = true
			break
		}
		c := Candidate{Values: map[string]any{}, Fields: map[string]FieldEvidence{}, Valid: true}
		for _, m := range d.Output.Mapping {
			f := fields[m.Key]
			rawField := f
			if m.Transform != nil {
				rawField = m.Transform.Input
			}
			var values []any
			present := true
			switch m.Source {
			case "input":
				v, ok := input[m.InputKey]
				present = ok
				if ok {
					if rawField.Multiple {
						if a, ok := v.([]any); ok {
							values = append([]any{}, a...)
						} else {
							values = []any{v}
						}
					} else {
						values = []any{v}
					}
				}
			case "constant":
				v, _ := capture.DecodeJSON(string(m.Value))
				if rawField.Multiple {
					if a, ok := v.([]any); ok {
						values = append([]any{}, a...)
					} else {
						values = []any{v}
					}
				} else {
					values = []any{v}
				}
			case "element":
				if doc.Format == "json" {
					values, e = jsonSelect(record, m.Locator.Expression)
				} else {
					if m.Locator.Strategy == "xpath" && strings.HasPrefix(m.Locator.Expression, "/") {
						return result, Invalid("output.mapping."+m.Key, "记录字段XPath必须相对当前记录，例如.//h1")
					}
					ns, err := nodes(record.(*html.Node), *m.Locator)
					e = err
					for _, n := range ns {
						var v any
						v = htmlValue(n, m, doc.URL)
						values = append(values, v)
					}
				}
				present = len(values) > 0
			}
			ev := FieldEvidence{MatchCount: len(values), Status: "valid"}
			var value any
			if e != nil {
				ev.Status = "invalid"
				ev.Message = e.Error()
			} else if !present {
				if len(f.Default) > 0 {
					value, _ = capture.DecodeJSON(string(f.Default))
				} else if f.Required {
					ev.Status = "missing"
					ev.Message = "未命中必填字段"
				} else {
					ev.Status = "missing"
				}
			} else {
				if !rawField.Multiple && len(values) > 1 {
					ev.Status = "invalid"
					ev.Message = "单值字段命中多个元素，请缩小规则或使用多值"
				}
				for i, v := range values {
					if text, ok := v.(string); ok {
						switch m.Clean {
						case "trim":
							v = strings.TrimSpace(text)
						case "whitespace":
							v = strings.Join(strings.Fields(text), " ")
						}
						values[i] = v
					}
				}
				if rawField.Multiple {
					value = values
				} else if len(values) > 0 {
					value = values[0]
				}
			}
			if ev.Status == "valid" {
				if m.Transform != nil && present {
					value, ev.Transform, e = m.Transform.Apply(ctx, value, f, m.Source == "element", "output."+d.Output.Port+".mapping."+m.Key+".transform")
					if e != nil {
						ev.Error = AsError(e)
					}
				} else {
					value, e = ValidateValue(value, f, m.Source == "element")
				}
				if e != nil {
					ev.Status = "invalid"
					ev.Message = e.Error()
				}
			}
			if ev.Status == "invalid" || ev.Status == "missing" && f.Required {
				c.Valid = false
			}
			if present || len(f.Default) > 0 {
				c.Values[m.Key] = value
			}
			c.Fields[m.Key] = ev
			e = nil
		}
		identity := []any{}
		for _, k := range d.Output.IdentityFields {
			v, ok := c.Values[k]
			if !ok || v == nil || v == "" {
				c.Valid = false
			} else {
				identity = append(identity, v)
			}
		}
		if c.Valid {
			c.Identity = Hash(identity)
		}
		if len(JSON(c.Values)) > 65536 {
			c.Valid = false
			c.Fields["_record"] = FieldEvidence{Status: "invalid", Message: "结果超过64KiB"}
		}
		result.Candidates = append(result.Candidates, c)
	}
	if d.Pagination.EmptyMarker != nil {
		if doc.Format == "json" {
			vs, err := jsonSelect(data, d.Pagination.EmptyMarker.Expression)
			if err != nil {
				return result, err
			}
			result.EmptyVerified = len(vs) == 1 && vs[0] == true
		} else {
			ns, err := nodes(root, *d.Pagination.EmptyMarker)
			if err != nil {
				return result, err
			}
			result.EmptyVerified = len(ns) > 0
		}
	}
	if d.Pagination.Next != nil {
		if doc.Format == "json" {
			vs, err := jsonSelect(data, d.Pagination.Next.Expression)
			if err != nil {
				return result, err
			}
			if len(vs) > 1 {
				return result, Fault("AMBIGUOUS_NEXT", "pagination", "下一页/游标命中多个值")
			}
			if len(vs) > 0 && vs[0] != nil {
				result.Next, e = Scalar(vs[0])
				if e != nil {
					return result, e
				}
				result.HasNext = result.Next != ""
			}
		} else {
			ns, err := nodes(root, *d.Pagination.Next)
			if err != nil {
				return result, err
			}
			if len(ns) > 1 {
				return result, Fault("AMBIGUOUS_NEXT", "pagination", "下一页定位命中多个元素")
			}
			result.HasNext = len(ns) > 0
			if len(ns) > 0 {
				result.Next = htmlValue(ns[0], Mapping{Extract: "link"}, doc.URL).(string)
			}
		}
	}
	// Include matched values so a rotating nonce elsewhere does not hide repeat pages.
	result.Hash = Hash(result.Candidates)
	return result, nil
}
func htmlValue(n *html.Node, m Mapping, base string) any {
	if m.Extract == "text" {
		return htmlquery.InnerText(n)
	}
	attribute := m.Attribute
	if m.Extract == "link" {
		attribute = "href"
		for n != nil && n.Data != "a" {
			n = n.Parent
		}
		if n == nil {
			return ""
		}
	}
	if m.Extract == "image" {
		attribute = "src"
		if n.Data != "img" {
			n = cascadia.Query(n, cascadia.MustCompile("img"))
			if n == nil {
				return ""
			}
		}
	}
	value := ""
	for _, a := range n.Attr {
		if a.Key == attribute {
			value = a.Val
			break
		}
	}
	if (m.Extract == "link" || m.Extract == "image") && value != "" {
		u, e := url.Parse(base)
		if e == nil {
			ref, err := url.Parse(value)
			if err == nil {
				target := u.ResolveReference(ref).String()
				if _, err = capture.ValidateURL(target); err == nil {
					return target
				}
			}
		}
		return ""
	}
	return value
}
