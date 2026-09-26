package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nekoimi/scrapio/internal/crawlpolicy"
	"github.com/nekoimi/scrapio/internal/script"
)

// Definition is the versioned, provider-neutral workflow document. Nodes are
// deliberately represented as JSON objects so new node configuration can be
// added without changing the database model or the browser protocol.
type Definition struct {
	Persistence  string              `json:"persistence,omitempty"`
	Trigger      Trigger             `json:"trigger"`
	Listing      *ListingOptions     `json:"listing,omitempty"`
	Budget       *crawlpolicy.Budget `json:"budget,omitempty"`
	Acquire      []Node              `json:"acquire,omitempty"`
	Nodes        []Node              `json:"nodes"`
	InputSchema  json.RawMessage     `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage     `json:"output_schema,omitempty"`
	Credentials  map[string]string   `json:"credentials,omitempty"`
}

// ListingOptions bounds a list -> detail crawl. URLs stay on the entry host.
type ListingOptions struct {
	DetailSelector string `json:"detail_selector"`
	NextSelector   string `json:"next_selector,omitempty"`
	MaxPages       int    `json:"max_pages"`
	MaxEmptyPages  int    `json:"max_empty_pages"`
}

type Trigger struct {
	Type        string       `json:"type"`
	Cron        string       `json:"cron,omitempty"`
	URL         string       `json:"url,omitempty"`
	Input       string       `json:"input,omitempty"`
	ProfileID   string       `json:"profile_id,omitempty"`
	Concurrency int          `json:"concurrency,omitempty"`
	Fetch       FetchOptions `json:"fetch,omitempty"`
}

type FetchOptions struct {
	Mode      string        `json:"mode,omitempty"`
	TimeoutMS int           `json:"timeout_ms,omitempty"`
	Actions   []FetchAction `json:"actions,omitempty"`
}

type FetchAction struct {
	Type      string `json:"type"`
	Selector  string `json:"selector,omitempty"`
	Value     string `json:"value,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	RunOn     string `json:"run_on,omitempty"`
}

type Node struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

var nodeTypes = map[string]struct{}{
	"trigger": {}, "acquire": {}, "navigate": {}, "discover": {},
	"extract": {}, "transform": {}, "validate": {}, "deduplicate": {},
	"script":  {},
	"persist": {}, "event": {}, "list": {}, "detail": {}, "pagination": {},
}

var triggerTypes = map[string]struct{}{"manual": {}, "cron": {}, "api": {}, "webhook": {}}

// ParseDefinition parses and validates a persisted workflow definition.
func ParseDefinition(raw string) (Definition, error) {
	var definition Definition
	if strings.TrimSpace(raw) == "" {
		return definition, fmt.Errorf("workflow definition is empty")
	}
	if err := json.Unmarshal([]byte(raw), &definition); err != nil {
		return definition, fmt.Errorf("invalid workflow definition: %w", err)
	}
	if err := definition.Validate(); err != nil {
		return definition, err
	}
	return definition, nil
}

// ParseExecutableDefinition rejects unknown structural fields at the publish
// boundary while retaining permissive historical reads through ParseDefinition.
func ParseExecutableDefinition(raw string) (Definition, error) {
	definition, err := ParseDefinition(raw)
	if err != nil {
		return definition, err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(new(Definition)); err != nil {
		return definition, fmt.Errorf("definition: %w", err)
	}
	return definition, definition.ValidateExecutable()
}

func (d Definition) Validate() error {
	if strings.TrimSpace(d.Trigger.Type) == "" {
		return fmt.Errorf("trigger.type is required")
	}
	if _, ok := triggerTypes[strings.ToLower(d.Trigger.Type)]; !ok {
		return fmt.Errorf("unsupported trigger type: %s", d.Trigger.Type)
	}
	if d.Trigger.Concurrency < 0 {
		return fmt.Errorf("trigger.concurrency must be greater than or equal to zero")
	}
	if len(d.Nodes) == 0 && len(d.Acquire) == 0 {
		return fmt.Errorf("workflow nodes are required")
	}
	seen := make(map[string]struct{}, len(d.Nodes)+len(d.Acquire))
	for _, node := range append(append([]Node{}, d.Acquire...), d.Nodes...) {
		if strings.TrimSpace(node.Name) == "" {
			return fmt.Errorf("node.name is required")
		}
		if _, ok := seen[node.Name]; ok {
			return fmt.Errorf("duplicate node name: %s", node.Name)
		}
		seen[node.Name] = struct{}{}
		if _, ok := nodeTypes[strings.ToLower(node.Type)]; !ok {
			return fmt.Errorf("unsupported node type %q", node.Type)
		}
		if err := validateNode(node); err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
	}
	if err := validateSchema(d.InputSchema, "input_schema"); err != nil {
		return err
	}
	if err := validateSchema(d.OutputSchema, "output_schema"); err != nil {
		return err
	}
	for name, ref := range d.Credentials {
		if strings.TrimSpace(name) == "" || !strings.HasPrefix(strings.TrimSpace(ref), "${secret:") || !strings.HasSuffix(strings.TrimSpace(ref), "}") {
			return fmt.Errorf("credential %q must use ${secret:name} reference", name)
		}
	}
	return nil
}

// ValidateExecutable is the publish/runtime boundary for the current worker.
// ParseDefinition stays permissive so historical definitions remain readable.
func (d Definition) ValidateExecutable() error {
	if err := d.Validate(); err != nil {
		return err
	}
	if d.Persistence != "" && d.Persistence != "records" {
		return fmt.Errorf("persistence: only records is supported")
	}
	if d.Trigger.Type != "manual" {
		return fmt.Errorf("trigger.type: %q is not executable; only manual is supported", d.Trigger.Type)
	}
	if d.Trigger.Cron != "" || d.Trigger.Input != "" || d.Trigger.Concurrency != 0 || d.Trigger.ProfileID != "" {
		return fmt.Errorf("trigger: cron, input, concurrency and profile_id are not executable")
	}
	if len(d.Credentials) != 0 {
		return fmt.Errorf("credentials: secret references are not resolved by the workflow worker")
	}
	if len(d.InputSchema) != 0 || len(d.OutputSchema) != 0 {
		return fmt.Errorf("input_schema/output_schema: runtime schema validation is not implemented")
	}
	if len(d.Acquire) != 0 {
		return fmt.Errorf("acquire: acquisition nodes are not executed by the workflow worker")
	}
	if len(d.Nodes) == 0 {
		return fmt.Errorf("nodes: at least one executable node is required")
	}
	if strings.TrimSpace(d.Trigger.URL) == "" {
		return fmt.Errorf("trigger.url: an entry URL is required for publication")
	}
	if err := validateHTTPURL(d.Trigger.URL); err != nil {
		return fmt.Errorf("trigger.url: %w", err)
	}
	budget := crawlpolicy.Defaults(d.Trigger.URL)
	if d.Budget != nil {
		budget = *d.Budget
	}
	if err := budget.Validate(d.Trigger.URL); err != nil {
		return err
	}
	if err := validateFetchOptions(d.Trigger.Fetch); err != nil {
		return fmt.Errorf("trigger.fetch: %w", err)
	}
	if d.Listing != nil {
		if d.Persistence != "records" {
			return fmt.Errorf("listing: records persistence is required")
		}
		if strings.TrimSpace(d.Listing.DetailSelector) == "" || d.Listing.MaxPages < 1 || d.Listing.MaxPages > 100 || d.Listing.MaxEmptyPages < 1 || d.Listing.MaxEmptyPages > 10 {
			return fmt.Errorf("listing: detail_selector, max_pages 1–100 and max_empty_pages 1–10 are required")
		}
		for _, selector := range []string{d.Listing.DetailSelector, d.Listing.NextSelector} {
			if strings.TrimSpace(selector) == "" {
				continue
			}
			if err := validateListingSelector(selector); err != nil {
				return fmt.Errorf("listing: %w", err)
			}
		}
		for i, action := range d.Trigger.Fetch.Actions {
			if action.Type == "navigate" {
				return fmt.Errorf("trigger.fetch.actions[%d]: navigate cannot override listing or detail task URLs", i)
			}
		}
	}
	hasExtract := false
	hasDiscover := false
	for i, node := range d.Nodes {
		path := fmt.Sprintf("nodes[%d]", i)
		if _, exists := node.Config["items_path"]; exists && d.Persistence != "records" {
			return fmt.Errorf("%s.config.items_path: requires records persistence", path)
		}
		switch node.Type {
		case "extract", "discover", "transform", "validate", "script":
		default:
			return fmt.Errorf("%s.type: %q is not executed by the workflow worker", path, node.Type)
		}
		if err := validateExecutableNode(node); err != nil {
			return fmt.Errorf("%s.%w", path, err)
		}
		if node.Type == "extract" {
			hasExtract = true
		}
		if node.Type == "discover" {
			hasDiscover = true
		}
	}
	if !hasExtract {
		return fmt.Errorf("nodes: extract is required to produce a resource")
	}
	// The current persistence adapter only writes magnet resources. Check the
	// final field names for every page role that must produce a resource.
	role := "trigger"
	if hasDiscover {
		role = "detail"
	}
	if d.Persistence != "records" && !d.producesPersistableFields(role) {
		return fmt.Errorf("nodes: %s pages cannot produce number, canonical_key or magnet links for persistence", role)
	}
	if d.Persistence == "records" {
		for _, n := range d.Nodes {
			if n.Type == "discover" {
				return fmt.Errorf("nodes: records workflows cannot use discover nodes; use listing")
			}
			if n.Type == "script" {
				if _, err := scriptContract(n.Config); err != nil {
					return fmt.Errorf("node %s: %w", n.Name, err)
				}
			}
		}
		if d.Nodes[0].Type != "extract" {
			return fmt.Errorf("nodes[0].type: records workflow must begin with extract")
		}
		for i, n := range d.Nodes {
			if i > 0 && n.Type == "extract" {
				return fmt.Errorf("nodes[%d].type: records workflow supports one extract node", i)
			}
		}
	}
	return nil
}

func (d Definition) producesPersistableFields(role string) bool {
	fields := map[string]bool{}
	for _, node := range d.Nodes {
		if !nodeApplies(node, role) {
			continue
		}
		switch node.Type {
		case "extract":
			if raw, ok := node.Config["fields"].([]any); ok {
				for _, item := range raw {
					if field, ok := item.(map[string]any); ok {
						if name, ok := field["name"].(string); ok {
							fields[name] = true
						}
					}
				}
			}
		case "transform":
			if operations, ok := node.Config["operations"].([]any); ok {
				for _, item := range operations {
					op, ok := item.(map[string]any)
					if !ok {
						continue
					}
					name, _ := op["field"].(string)
					switch op["op"] {
					case "rename":
						if fields[name] {
							delete(fields, name)
							to, _ := op["to"].(string)
							fields[to] = true
						}
					case "set", "default":
						fields[name] = true
					case "delete":
						delete(fields, name)
					}
				}
			}
		}
	}
	for _, name := range []string{"number", "canonical_key", "links", "optimal_link", "optimalLink"} {
		if fields[name] {
			return true
		}
	}
	return false
}

func validateNode(node Node) error {
	if runOn, ok := node.Config["run_on"]; ok && runOn != nil {
		switch value := runOn.(type) {
		case string:
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("run_on must not be empty")
			}
		case []any:
			if len(value) == 0 {
				return fmt.Errorf("run_on must not be empty")
			}
		default:
			return fmt.Errorf("run_on must be a string or array")
		}
	}
	switch strings.ToLower(strings.TrimSpace(node.Type)) {
	case "extract":
		fields, ok := node.Config["fields"]
		if !ok || fields == nil {
			return fmt.Errorf("extract.fields is required")
		}
		if _, ok := fields.([]any); !ok {
			return fmt.Errorf("extract.fields must be an array")
		}
	case "discover", "validate":
		if fields, ok := node.Config["fields"]; ok && fields != nil {
			if _, ok := fields.([]any); !ok {
				return fmt.Errorf("%s.fields must be an array", strings.ToLower(node.Type))
			}
		}
	case "transform":
		if operations, ok := node.Config["operations"]; ok && operations != nil {
			if _, ok := operations.([]any); !ok {
				return fmt.Errorf("transform.operations must be an array")
			}
		}
	case "script":
		if err := script.Validate(stringConfigValue(node.Config["script"])); err != nil {
			return err
		}
	}
	return nil
}

func stringConfigValue(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func validateSchema(raw json.RawMessage, name string) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s must be a JSON object: %w", name, err)
	}
	if value == nil {
		return fmt.Errorf("%s must be a JSON object", name)
	}
	return nil
}
