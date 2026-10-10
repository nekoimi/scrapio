package trial

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
)

// Checkpoint is a durable diagnostic boundary, not authorization to replay
// actions or proof that formal records have committed.
type Checkpoint struct {
	StepID           string `json:"step_id"`
	ListPage         int    `json:"list_page"`
	DocumentID       string `json:"document_id"`
	URL              string `json:"source_url"`
	Hash             string `json:"content_hash"`
	State            string `json:"state"`
	Next             string `json:"next_target"`
	Reason           string `json:"stop_reason"`
	Pages            int    `json:"pages"`
	ListPages        int    `json:"list_pages"`
	Candidates       int    `json:"candidates"`
	Details          int    `json:"details"`
	DuplicateRecords int    `json:"duplicate_records"`
}

func URLKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	return u.String()
}

// Page identity ignores changing page chrome and includes detail URLs. Sorting
// makes a reordering of the same records recognizable without rounding JSON.
func PageFingerprint(r extraction.Result, paths []extraction.Path) string {
	parts := []string{}
	for _, record := range r.Records {
		raw, _ := json.Marshal(record.Values)
		parts = append(parts, string(raw))
	}
	for _, p := range paths {
		parts = append(parts, "detail:"+URLKey(p.URL)+":"+p.Error)
	}
	sort.Strings(parts)
	raw, _ := json.Marshal(parts)
	return capture.Hash(raw)
}

// Equal key+incoming values are dropped across documents, while duplicate or
// conflicting keys within a page and differing values remain visible to output
// validation. Diagnostic records themselves are always retained.
func OutputIndices(r extraction.Result, s output.Schema, m []output.Mapping, seen map[string]bool) ([]int, int) {
	indices := []int{}
	duplicates := 0
	newKeys := []string{}
	for _, record := range r.Records {
		p, issues := output.Prepare(s, m, record)
		key := p.Key + ":" + p.Hash
		if len(issues) == 0 && seen[key] {
			duplicates++
			continue
		}
		indices = append(indices, record.Index)
		if len(issues) == 0 {
			newKeys = append(newKeys, key)
		}
	}
	for _, key := range newKeys {
		seen[key] = true
	}
	return indices, duplicates
}

// Returning to an accumulated list must preserve its records and next target.
// Changes to unrelated page chrome alone do not invalidate that boundary.
func SameList(ctx context.Context, before, after FixedDocument, plan extraction.Plan) (bool, error) {
	if before.URL != after.URL || after.Hash != capture.Hash([]byte(after.Content)) {
		return false, nil
	}
	if before.Hash == after.Hash {
		return true, nil
	}
	fingerprints := []string{}
	next := []string{}
	for _, doc := range []FixedDocument{before, after} {
		input := extraction.Input{Content: doc.Content, Format: doc.Format, URL: doc.URL, BaseURL: doc.BaseURL, Stage: "list"}
		r, err := extraction.Extract(ctx, input, plan)
		if err != nil {
			return false, err
		}
		paths, err := extraction.DetailPaths(ctx, input, plan)
		if err != nil {
			return false, err
		}
		fingerprints = append(fingerprints, PageFingerprint(r, paths))
		n, err := extraction.NextPath(ctx, input, plan)
		if err != nil {
			return false, err
		}
		next = append(next, n)
	}
	return fingerprints[0] == fingerprints[1] && next[0] == next[1], nil
}
