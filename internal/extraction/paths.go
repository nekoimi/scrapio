package extraction

import (
	"context"
	"errors"
	"strings"

	"github.com/nekoimi/scrapio/internal/editor"
	"golang.org/x/net/html"
)

type Path struct {
	Index int    `json:"record_index"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// DetailPaths uses exactly the saved-document selector/scoping rules used by Extract.
func DetailPaths(ctx context.Context, input Input, plan Plan) ([]Path, error) {
	if plan.Kind != "record_set" || plan.HTML.Detail == nil {
		return nil, nil
	}
	root, err := html.Parse(strings.NewReader(input.Content))
	if err != nil {
		return nil, err
	}
	records := []*html.Node{root}
	if plan.HTML.Mode == "repeated" {
		records, _, err = selectNodes(ctx, root, plan.HTML.Locator, plan.HTML.MaxRecords)
		if err != nil {
			return nil, err
		}
	}
	base := input.BaseURL
	if base == "" {
		base = input.URL
		if nodes, _, _ := selectNodes(ctx, root, &editor.Locator{Strategy: "css", Expression: "base[href]"}, 1); len(nodes) > 0 {
			if v, code := resolveURL(attr(nodes[0], "href"), input.URL); code == "" {
				base = v
			}
		}
	}
	paths := []Path{}
	for i, n := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nodes, truncated, err := selectNodes(ctx, n, plan.HTML.Detail.Locator, 2)
		path := Path{Index: i}
		if err != nil || truncated || len(nodes) != 1 {
			path.Error = "DETAIL_LOCATOR_NOT_UNIQUE"
		} else {
			path.URL, path.Error = resolveURL(attr(nodes[0], "href"), base)
			if attr(nodes[0], "href") == "" {
				path.URL = ""
				path.Error = "DETAIL_LINK_REQUIRED"
			}
		}
		paths = append(paths, path)
	}
	return paths, nil
}
func NextPath(ctx context.Context, input Input, plan Plan) (string, error) {
	if plan.HTML.NextPage == nil {
		return "", nil
	}
	root, err := html.Parse(strings.NewReader(input.Content))
	if err != nil {
		return "", err
	}
	nodes, truncated, err := selectNodes(ctx, root, plan.HTML.NextPage.Locator, 2)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", nil
	}
	if truncated || len(nodes) != 1 {
		return "", errors.New("NEXT_LOCATOR_NOT_UNIQUE")
	}
	if _, disabled := attribute(nodes[0], "disabled"); disabled || attr(nodes[0], "aria-disabled") == "true" {
		return "", nil
	}
	if plan.HTML.NextPage.Kind == "click" || plan.HTML.NextPage.Kind == "load_more" {
		return "click", nil
	}
	base := input.BaseURL
	if base == "" {
		base = input.URL
		if bases, _, _ := selectNodes(ctx, root, &editor.Locator{Strategy: "css", Expression: "base[href]"}, 1); len(bases) > 0 {
			if value, code := resolveURL(attr(bases[0], "href"), input.URL); code == "" {
				base = value
			}
		}
	}
	target, code := resolveURL(attr(nodes[0], "href"), base)
	if code != "" || attr(nodes[0], "href") == "" {
		return "", errors.New("NEXT_LINK_REQUIRED")
	}
	return target, nil
}
