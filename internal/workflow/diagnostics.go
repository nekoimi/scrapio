package workflow

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/antchfx/htmlquery"
)

// FieldDiagnostic calls the same extraction code as execution for each field.
// Raw excludes cleaning, regex and type conversion; Converted includes them.
type FieldDiagnostic struct {
	Name       string `json:"name"`
	RulePath   string `json:"rule_path"`
	Selector   string `json:"selector"`
	MatchCount int    `json:"match_count"`
	Raw        any    `json:"raw,omitempty"`
	Converted  any    `json:"converted,omitempty"`
	Error      string `json:"error,omitempty"`
}

func DiagnoseExtractNode(node Node, nodeIndex int, result FetchResult) []FieldDiagnostic {
	rules, err := fieldRules(node.Config["fields"])
	if err != nil {
		return nil
	}
	content, contentType := result.HTML, "html"
	if strings.EqualFold(stringValue(node.Config["content_type"]), "json") || (content == "" && result.JSON != "") || usesJSONPath(rules) {
		content, contentType = result.JSON, "json"
	}
	rows := make([]FieldDiagnostic, 0, len(rules))
	for i, rule := range rules {
		row := FieldDiagnostic{Name: rule.Name, RulePath: "nodes[" + strconv.Itoa(nodeIndex) + "].config.fields[" + strconv.Itoa(i) + "]", Selector: rule.Selector}
		row.MatchCount = countMatches(contentType, content, rule)
		rawRule := rule
		rawRule.Regex, rawRule.Clean, rawRule.Type = "", "", ""
		rawRule.Required, rawRule.Default = false, nil
		if values, err := Extract(ExtractRequest{ContentType: contentType, Content: content, Fields: []FieldRule{rawRule}}); err == nil {
			row.Raw = values[rule.Name]
		}
		if values, err := Extract(ExtractRequest{ContentType: contentType, Content: content, Fields: []FieldRule{rule}}); err == nil {
			row.Converted = values[rule.Name]
		} else {
			row.Error = err.Error()
		}
		rows = append(rows, row)
	}
	return rows
}

func countMatches(contentType, content string, rule FieldRule) int {
	kind := strings.ToLower(strings.TrimSpace(rule.SelectorType))
	if kind == "" {
		if strings.HasPrefix(strings.TrimSpace(rule.Selector), "$") || contentType == "json" {
			kind = "jsonpath"
		} else {
			kind = "css"
		}
	}
	switch kind {
	case "jsonpath", "json_path":
		var root any
		if json.Unmarshal([]byte(content), &root) != nil {
			return 0
		}
		value, err := jsonPathValue(root, rule.Selector)
		if err != nil || value == nil {
			return 0
		}
		if items, ok := value.([]any); ok {
			return len(items)
		}
		return 1
	case "xpath":
		root, err := htmlquery.Parse(strings.NewReader(content))
		if err != nil {
			return 0
		}
		return len(htmlquery.Find(root, rule.Selector))
	default:
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
		if err != nil {
			return 0
		}
		return doc.Find(rule.Selector).Length()
	}
}
