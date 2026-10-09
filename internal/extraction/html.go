package extraction

import (
	"context"
	"errors"
	"github.com/andybalholm/cascadia"
	"github.com/antchfx/htmlquery"
	"github.com/antchfx/xpath"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"golang.org/x/net/html"
	"net/url"
	"strconv"
	"strings"
)

func extractHTML(ctx context.Context, input Input, plan Plan, result *Result) error {
	root, err := html.Parse(strings.NewReader(input.Content))
	if err != nil {
		return err
	}
	// Captures are sanitized, but enforce budgets for direct interpreter users.
	count := 0
	var check func(*html.Node, int) error
	check = func(node *html.Node, depth int) error {
		count++
		if count > 100000 || depth > 256 {
			return errors.New("HTML node/depth budget exceeded")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := check(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err = check(root, 0); err != nil {
		return err
	}
	base := input.BaseURL
	if base == "" {
		base = input.URL
		if nodes, _, _ := selectNodes(ctx, root, &editor.Locator{Strategy: "css", Expression: "base[href]"}, 1); len(nodes) > 0 {
			if candidate, code := resolveURL(attr(nodes[0], "href"), input.URL); code == "" {
				base = candidate
			}
		}
	}
	fields := plan.HTML.Fields
	records := []*html.Node{root}
	truncated := false
	if input.Stage == "detail" {
		if plan.HTML.Detail == nil {
			return errors.New("detail preview requires an explicit detail path")
		}
		fields = plan.HTML.DetailFields
	} else if plan.HTML.Mode == "repeated" {
		records, truncated, err = selectNodes(ctx, root, plan.HTML.Locator, 1000)
		if err != nil {
			return errors.New("invalid record locator or unsupported scope")
		}
	}
	result.MatchCount = len(records)
	result.MatchCountLowerBound = truncated
	result.Truncated = truncated || len(records) > plan.HTML.MaxRecords
	for index, node := range records {
		if index >= plan.HTML.MaxRecords {
			break
		}
		record := newRecord(index)
		for _, rule := range fields {
			if err = ctx.Err(); err != nil {
				return err
			}
			matches := []*html.Node{node}
			limited := false
			var selectErr error
			if rule.Locator != nil {
				matches, limited, selectErr = selectNodes(ctx, node, rule.Locator, 50)
			}
			raw := []any{}
			prepared := []any{}
			codes := []string{}
			if selectErr != nil {
				codes = append(codes, "INVALID_LOCATOR")
			}
			for _, matched := range matches {
				var value any
				var original any
				switch rule.Extract {
				case "text":
					value = visibleText(matched)
				case "attribute":
					if capture.IsSensitiveKey(rule.Attribute) || strings.EqualFold(rule.Attribute, "value") {
						codes = append(codes, "SENSITIVE_ATTRIBUTE")
					} else {
						value, _ = attribute(matched, rule.Attribute)
					}
				case "link", "image":
					name := "href"
					if rule.Extract == "image" {
						name = "src"
					}
					v, found := attribute(matched, name)
					if found {
						original = v
					}
					if found && v != "" {
						resolved, code := resolveURL(v, base)
						if code != "" {
							codes = append(codes, code)
						} else {
							value = resolved
						}
					}
				}
				if rule.Extract != "link" && rule.Extract != "image" {
					original = value
				}
				raw = append(raw, original)
				prepared = append(prepared, value)
			}
			f := processPrepared(rule.Name, rule.Options, raw, prepared, limited)
			f.Locator = rule.Locator
			f.Errors = append(f.Errors, codes...)
			f.Valid = f.Valid && len(codes) == 0
			addField(&record, f)
		}
		if err := addRecord(result, record); err != nil {
			return err
		}
	}
	result.Warnings = append(result.Warnings, "SAVED_HTML_ONLY_NO_NAVIGATION", "NO_IFRAME_SHADOW_DOM_OR_COMPUTED_VISIBILITY")
	return nil
}

func selectNodes(ctx context.Context, root *html.Node, locator *editor.Locator, limit int) ([]*html.Node, bool, error) {
	if locator == nil {
		return []*html.Node{root}, false, nil
	}
	nodes := []*html.Node{}
	if locator.Strategy == "xpath" {
		expression, err := xpath.Compile(locator.Expression)
		if err != nil {
			return nil, false, err
		}
		iterator := expression.Select(htmlquery.CreateXPathNavigator(root))
		for iterator.MoveNext() {
			if err = ctx.Err(); err != nil {
				return nil, false, err
			}
			nav := iterator.Current().(*htmlquery.NodeNavigator)
			if nav.NodeType() != xpath.ElementNode {
				return nil, false, errors.New("XPath must select elements")
			}
			node := nav.Current()
			if !within(node, root) {
				return nil, false, errors.New("locator leaves record scope")
			}
			if len(nodes) >= limit {
				return nodes, true, nil
			}
			nodes = append(nodes, node)
		}
		return nodes, false, nil
	}
	if locator.Strategy != "css" {
		return nil, false, errors.New("unsupported locator strategy")
	}
	expression := locator.Expression
	if strings.HasPrefix(expression, ":scope") {
		if root.Type == html.DocumentNode {
			for child := root.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode {
					root = child
					break
				}
			}
		}
		if expression == ":scope" {
			return []*html.Node{root}, false, nil
		}
		if !strings.HasPrefix(expression, ":scope > ") && !strings.HasPrefix(expression, ":scope ") {
			return nil, false, errors.New("unsupported scope selector")
		}
		name := "data-scrapio-extraction-scope"
		// Pick a marker name absent from the complete parsed document, so
		// existing page attributes cannot change :scope matching semantics.
		top := root
		for top.Parent != nil {
			top = top.Parent
		}
		used := map[string]bool{}
		var collect func(*html.Node)
		collect = func(n *html.Node) {
			for _, a := range n.Attr {
				used[a.Key] = true
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				collect(child)
			}
		}
		collect(top)
		for index := 1; used[name]; index++ {
			name = "data-scrapio-extraction-scope-" + strconv.Itoa(index)
		}
		prior := root.Attr
		root.Attr = append(append([]html.Attribute{}, prior...), html.Attribute{Key: name, Val: "current"})
		defer func() { root.Attr = prior }()
		expression = `[` + name + `="current"]` + strings.TrimPrefix(expression, ":scope")
	}
	matcher, err := cascadia.Compile(expression)
	if err != nil {
		return nil, false, err
	}
	var walk func(*html.Node) error
	full := false
	walk = func(node *html.Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && matcher.Match(child) {
				if len(nodes) >= limit {
					full = true
					return nil
				}
				nodes = append(nodes, child)
			}
			if err := walk(child); err != nil {
				return err
			}
			if full {
				return nil
			}
		}
		return nil
	}
	if err = walk(root); err != nil {
		return nil, false, err
	}
	return nodes, full, nil
}
func within(node, root *html.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current == root {
			return true
		}
	}
	return false
}
func attribute(node *html.Node, key string) (string, bool) {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}
func attr(node *html.Node, key string) string { v, _ := attribute(node, key); return v }
func visibleText(node *html.Node) string {
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "input", "textarea":
				return
			}
			if _, hidden := attribute(n, "hidden"); hidden {
				return
			}
			if n.Data == "br" {
				out.WriteString("\n")
			}
		}
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return out.String()
}
func resolveURL(raw, base string) (string, string) {
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "INVALID_URL"
	}
	parent, err := url.Parse(base)
	if err != nil {
		return "", "INVALID_URL"
	}
	resolved := parent.ResolveReference(reference)
	resolved.Fragment = ""
	if _, err = capture.ValidateURL(resolved.String()); err != nil {
		return "", "INVALID_URL"
	}
	return resolved.String(), ""
}
