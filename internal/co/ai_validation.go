package co

// This file contains the deterministic half of O5-07.  The model may propose
// a candidate, but only the collector interpreter and task mapper can mark it
// as valid.  Keeping this code outside the gateway client makes it impossible
// for a successful HTTP response to become an adopted rule by itself.

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/collector"
	"golang.org/x/net/html"
)

var aiSecretKey = regexp.MustCompile(`(?i)(pass(word)?|token|secret|cookie|authorization|credential|api[_-]?key|nonce)`)
var aiSecretAttr = regexp.MustCompile(`(?i)(value|nonce|data-token|authorization|cookie)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)

func aiRedactDocument(format, content string) string {
	if format != "html" {
		value, err := capture.DecodeJSON(content)
		if err != nil {
			return "[JSON样例不完整，请选择较小样例]"
		}
		return collector.JSON(aiRedactValue(value))
	}
	// Keep structure and labels useful to the model while removing executable
	// or credential-bearing parts of an inspected page.
	content = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>|<form\b[^>]*>.*?</form\s*>|<iframe\b[^>]*>.*?</iframe\s*>`).ReplaceAllString(content, "")
	content = aiSecretAttr.ReplaceAllString(content, `$1="[REDACTED]"`)
	content = regexp.MustCompile(`https?://[^\s"'<>]+`).ReplaceAllStringFunc(content, aiSafeURL)
	root, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "[无法脱敏的HTML样例]"
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		attrs := []html.Attribute{}
		for _, attr := range n.Attr {
			if aiSecretKey.MatchString(attr.Key) || strings.HasPrefix(strings.ToLower(attr.Key), "on") || attr.Key == "value" {
				continue
			}
			if attr.Key == "href" || attr.Key == "src" {
				if u, e := url.Parse(attr.Val); e == nil {
					u.User = nil
					u.RawQuery = ""
					u.Fragment = ""
					attr.Val = u.String()
				}
			}
			attrs = append(attrs, attr)
		}
		n.Attr = attrs
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	var out bytes.Buffer
	_ = html.Render(&out, root)
	return out.String()
}

func aiFieldHints(fields []collector.Field) []any {
	result := []any{}
	for _, f := range fields {
		result = append(result, map[string]any{"key": f.Key, "label": f.Label, "type": f.Type, "required": f.Required, "nullable": f.Nullable, "multiple": f.Multiple})
	}
	return result
}

func aiSafeURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return value
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

func aiRedactValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			if aiSecretKey.MatchString(k) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = aiRedactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = aiRedactValue(item)
		}
		return out
	case string:
		return aiSafeURL(v)
	default:
		return value
	}
}

func aiPortDefinition(d collector.Definition, port string) (collector.Definition, error) {
	return d.ForPort(port)
}

func applyAISelectors(d collector.Definition, srcPort string, edits []AISelectorEdit) (collector.Definition, error) {
	var copy collector.Definition
	if err := collector.Strict([]byte(collector.JSON(d)), &copy); err != nil {
		return d, err
	}
	d = copy
	if len(edits) == 0 || len(edits) > 10 {
		return d, collector.Invalid("ai.selectors", "候选需要1..10个字段定位修改")
	}
	// Work on the selected port and then copy only its output rules back to the
	// full definition, preserving every other output port byte-for-byte.
	set := func(output *collector.Output) error {
		fields := map[string]bool{}
		for _, f := range output.Fields {
			fields[f.Key] = true
		}
		seen := map[string]bool{}
		for _, edit := range edits {
			if edit.Field == "" || seen[edit.Field] {
				return collector.Invalid("ai.selectors."+edit.Field, "字段不存在或重复建议")
			}
			seen[edit.Field] = true
			if !fields[edit.Field] {
				return collector.Invalid("ai.selectors."+edit.Field, "AI不能新增字段")
			}
			if edit.Locator.Strategy != "css" && edit.Locator.Strategy != "xpath" && edit.Locator.Strategy != "jsonpath" {
				return collector.Invalid("ai.selectors."+edit.Field, "不支持的定位策略")
			}
			if err := edit.Locator.Validate(d.Fetch.Kind == "http_json"); err != nil {
				return collector.Invalid("ai.selectors."+edit.Field, err.Error())
			}
			if edit.Extract != "text" && edit.Extract != "link" && edit.Extract != "image" && edit.Extract != "attribute" {
				return collector.Invalid("ai.selectors."+edit.Field, "不支持的提取方式")
			}
			if edit.Extract == "attribute" && (edit.Attribute == "" || len(edit.Attribute) > 128) {
				return collector.Invalid("ai.selectors."+edit.Field, "属性名无效")
			}
			changed := false
			for i := range output.Mapping {
				m := &output.Mapping[i]
				if m.Key != edit.Field {
					continue
				}
				if m.Source != "element" || m.Locator == nil {
					return collector.Invalid("ai.selectors."+edit.Field, "只有已有元素字段允许接受定位建议")
				}
				loc := edit.Locator
				m.Locator = &loc
				m.Extract, m.Attribute = edit.Extract, edit.Attribute
				changed = true
			}
			if !changed {
				return collector.Invalid("ai.selectors."+edit.Field, "字段没有现有元素映射")
			}
		}
		return nil
	}
	if srcPort == "records" {
		if err := set(&d.Output); err != nil {
			return d, err
		}
	} else {
		found := false
		for i := range d.AdditionalOutputs {
			if d.AdditionalOutputs[i].Output.Port == srcPort {
				if err := set(&d.AdditionalOutputs[i].Output); err != nil {
					return d, err
				}
				found = true
			}
		}
		if !found {
			return d, collector.Invalid("ai.port", "采集器输出端口不存在")
		}
	}
	if err := d.Validate(); err != nil {
		return d, err
	}
	return d, nil
}

func validateAICandidate(ctx context.Context, suggestion AISuggestion, candidate AICandidate) aiValidation {
	if len(candidate.Explanation) > 4000 || len(candidate.Selectors) > 10 || len(candidate.Bindings) > 10 {
		return aiFailure("预算", fmt.Errorf("候选超过字段或解释长度预算"))
	}
	if suggestion.Kind != "selectors" && len(candidate.Selectors) > 0 || suggestion.Kind != "mapping" && len(candidate.Bindings) > 0 {
		return aiFailure("范围", fmt.Errorf("候选包含本操作不允许的修改"))
	}
	var src aiSource
	if err := collector.Strict([]byte(suggestion.Source), &src); err != nil {
		return aiFailure("样例", err)
	}
	switch suggestion.Kind {
	case "selectors":
		updated, err := applyAISelectors(src.Definition, src.Port, candidate.Selectors)
		if err != nil {
			return aiFailure("字段定位", err)
		}
		one, err := aiPortDefinition(updated, src.Port)
		if err != nil {
			return aiFailure("字段定位", err)
		}
		if len(src.Documents) == 0 {
			return aiFailure("样例", fmt.Errorf("没有可验证页面"))
		}
		evidence := make([]any, 0, len(src.Documents))
		original, _ := src.Definition.ForPort(src.Port)
		actual := []map[string]any{}
		for _, doc := range src.Documents {
			if (doc.Format == "json") != (one.Fetch.Kind == "http_json") {
				return aiFailure("样例", fmt.Errorf("页面格式与获取方式不匹配"))
			}
			page, e := collector.Extract(ctx, one, src.Input, doc, 100)
			if e != nil {
				return aiFailure("字段定位", e)
			}
			valid := 0
			for _, row := range page.Candidates {
				if !row.Valid {
					return aiFailure("字段定位", fmt.Errorf("样例仍含无效记录，请查看字段类型和身份规则"))
				}
				for _, edit := range candidate.Selectors {
					if len(edit.Reason) > 1000 || row.Fields[edit.Field].MatchCount == 0 || row.Fields[edit.Field].Status != "valid" {
						return aiFailure("字段定位", fmt.Errorf("字段 %q 未准确命中每条样例记录", edit.Field))
					}
				}
				valid++
				actual = append(actual, row.Values)
			}
			if valid == 0 {
				return aiFailure("字段定位", fmt.Errorf("样例没有有效输出"))
			}
			before, _ := collector.Extract(ctx, original, src.Input, doc, 100)
			evidence = append(evidence, map[string]any{"url": aiSafeURL(doc.URL), "page_hash": page.Hash, "actual_count": len(page.Candidates), "truncated": page.Truncated, "before_candidates": aiRedactValue(aiJSONValue(before.Candidates[:minAI(len(before.Candidates), 10)])), "candidates": aiRedactValue(aiJSONValue(page.Candidates[:minAI(len(page.Candidates), 10)]))})
		}
		expected := []map[string]any{}
		for _, row := range src.Expected {
			if len(src.Definition.AdditionalOutputs) > 0 {
				if row["port"] == src.Port {
					if values, ok := row["values"].(map[string]any); ok {
						expected = append(expected, values)
					}
				}
			} else {
				expected = append(expected, row)
			}
		}
		comparison := map[string]any{"expected_count": len(expected), "actual_count": len(actual), "exact_match": collector.Hash(expected) == collector.Hash(actual), "expected": aiRedactValue(aiJSONValue(expected[:minAI(len(expected), 10)])), "scope": "saved snapshots only; inspect changes before accepting"}
		diff := map[string]any{"before": original.Output.Mapping, "after": one.Output.Mapping, "selectors": candidate.Selectors, "sample_comparison": comparison}
		return aiValidation{Passed: true, Hash: collector.Hash(map[string]any{"kind": suggestion.Kind, "definition": collector.Hash(updated), "evidence": evidence}), Evidence: evidence, Diff: aiRedactValue(aiJSONValue(diff))}
	case "mapping":
		edge := src.Edge
		edge.Mapping = copyAIMapping(edge.Mapping)
		allowed := map[string]collector.Field{}
		for _, field := range src.Down.InputFields {
			allowed[field.Key] = field
		}
		seen := map[string]bool{}
		for _, edit := range candidate.Bindings {
			field, ok := allowed[edit.Field]
			if !ok || seen[edit.Field] || len(edit.Reason) > 1000 || field.Multiple || field.Type == "json" {
				return aiFailure("映射", fmt.Errorf("下游字段 %q 不允许建议映射", edit.Field))
			}
			seen[edit.Field] = true
			if !strings.HasPrefix(edit.Path, "input.") && !strings.HasPrefix(edit.Path, "output.") {
				return aiFailure("映射", fmt.Errorf("字段路径必须是input.X或output.X"))
			}
			edge.Mapping[edit.Field] = collector.Binding{Kind: "field", Path: edit.Path}
		}
		if len(candidate.Bindings) == 0 || len(candidate.Bindings) > 10 {
			return aiFailure("映射", fmt.Errorf("候选需要1..10个字段映射"))
		}
		if err := validateEdge(edge, src.Definition, src.Down); err != nil {
			return aiFailure("映射", err)
		}
		rows, err := mapEdgeContext(ctx, edge, src.Down, src.Input, src.Output, false)
		if err != nil {
			return aiFailure("映射", err)
		}
		ready := 0
		for _, row := range rows {
			if row.Decision == "ready" {
				ready++
			}
		}
		if ready == 0 {
			return aiFailure("映射", fmt.Errorf("样例没有可执行的下游输入"))
		}
		evidence := map[string]any{"rows": aiRedactValue(aiJSONValue(rows[:minAI(len(rows), 10)])), "mapping_hash": collector.Hash(edge)}
		return aiValidation{Passed: true, Hash: collector.Hash(evidence), Evidence: evidence, Diff: aiRedactValue(aiJSONValue(map[string]any{"bindings": candidate.Bindings, "before": src.Edge.Mapping, "after": edge.Mapping}))}
	case "failure":
		if strings.TrimSpace(candidate.Explanation) == "" {
			return aiFailure("解释", fmt.Errorf("模型没有提供解释"))
		}
		// Failure explanations are review-only. They get an evidence hash so the
		// UI can show exactly what was explained, but AIControl never applies them.
		evidence := map[string]any{"kind": "failure", "base_hash": suggestion.BaseHash, "candidate": candidate, "prompt_version": aiPromptVersion}
		return aiValidation{Passed: true, Hash: collector.Hash(evidence), Evidence: map[string]any{"structured_diagnostic": true}, Diff: map[string]any{"explanation_only": true}}
	default:
		return aiFailure("候选", fmt.Errorf("不支持的AI类型"))
	}
}

func copyAIMapping(mapping map[string]collector.Binding) map[string]collector.Binding {
	copy := map[string]collector.Binding{}
	for key, value := range mapping {
		copy[key] = value
	}
	return copy
}

func aiJSONValue(value any) any {
	var v any
	_ = collector.Strict([]byte(collector.JSON(value)), &v)
	return v
}

func minAI(a, b int) int {
	if a < b {
		return a
	}
	return b
}
