package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nekoimi/scrapio/internal/field"
	"regexp"
)

type FieldRule struct {
	field.Options
	Name      string   `json:"name"`
	Locator   *Locator `json:"locator,omitempty"`
	Extract   string   `json:"extract"`
	Attribute string   `json:"attribute,omitempty"`
}
type DetailRule struct {
	Locator        *Locator `json:"locator"`
	ReturnStrategy string   `json:"return_strategy"`
	MaxDetails     int      `json:"max_details"`
}
type NextPageRule struct {
	Locator  *Locator `json:"locator"`
	Kind     string   `json:"kind"`
	MaxPages int      `json:"max_pages"`
}
type RecordPlan struct {
	Mode         string        `json:"mode"`
	Locator      *Locator      `json:"locator,omitempty"`
	MaxRecords   int           `json:"max_records"`
	Fields       []FieldRule   `json:"fields"`
	Detail       *DetailRule   `json:"detail,omitempty"`
	DetailFields []FieldRule   `json:"detail_fields,omitempty"`
	NextPage     *NextPageRule `json:"next_page,omitempty"`
}

func validLocator(locator *Locator, relative bool) bool {
	return locator != nil && (locator.Strategy == "css" || locator.Strategy == "xpath") && locator.Expression != "" && len(locator.Expression) <= 2048 && (!relative || locator.Strategy != "xpath" || locator.Expression[0] == '.')
}

func (p RecordPlan) Validate() error {
	if p.Mode != "single" && p.Mode != "repeated" || p.MaxRecords < 1 || p.MaxRecords > 20 {
		return errors.New("record mode and max_records (1..20) required")
	}
	if p.Mode == "repeated" && !validLocator(p.Locator, false) || p.Mode == "single" && p.Locator != nil {
		return errors.New("repeated records require a locator; single page uses document scope")
	}
	if len(p.Fields) > 30 || len(p.DetailFields) > 30 {
		return errors.New("at most 30 fields per page role")
	}
	if p.Fields == nil {
		return errors.New("fields must be an array, including when empty")
	}
	namePattern := regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_-]{0,63}$`)
	for role, fields := range [][]FieldRule{p.Fields, p.DetailFields} {
		seen := map[string]bool{}
		keys := map[string]bool{}
		for _, f := range fields {
			if err := f.Options.Validate(); err != nil {
				return fmt.Errorf("field %s: %w", f.Name, err)
			}
			if !namePattern.MatchString(f.Name) || seen[f.Name] {
				return fmt.Errorf("invalid or duplicate field name: %s", f.Name)
			}
			seen[f.Name] = true
			key := f.Key
			if key == "" {
				key = f.Name
			}
			if keys[key] {
				return errors.New("field_key must be unique within a page role")
			}
			keys[key] = true
			if f.Locator != nil && !validLocator(f.Locator, role == 0 && p.Mode == "repeated") {
				return fmt.Errorf("invalid relative locator for field %s", f.Name)
			}
			switch f.Extract {
			case "text", "link", "image":
			case "attribute":
				if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`).MatchString(f.Attribute) || f.Attribute == "value" {
					return errors.New("attribute name invalid; input values are not supported")
				}
			default:
				return errors.New("unsupported extraction kind")
			}
		}
	}
	if p.Detail != nil && (!validLocator(p.Detail.Locator, p.Mode == "repeated") || p.Detail.ReturnStrategy != "back" && p.Detail.ReturnStrategy != "navigate-list" || p.Detail.MaxDetails < 1 || p.Detail.MaxDetails > 5) {
		return errors.New("detail locator, explicit return strategy and max_details (1..5) required")
	}
	if p.Detail == nil && len(p.DetailFields) > 0 {
		return errors.New("detail fields require a detail path")
	}
	if p.NextPage != nil && (!validLocator(p.NextPage.Locator, false) || p.NextPage.Kind != "link" && p.NextPage.Kind != "click" || p.NextPage.MaxPages != 2) {
		return errors.New("next page requires a locator, link/click kind and max_pages=2")
	}
	return nil
}

func RecordPlanFromStep(step map[string]any) (RecordPlan, error) {
	var plan RecordPlan
	if step["type"] != "record_set" {
		return plan, errors.New("not a record_set step")
	}
	data, err := json.Marshal(step["config"])
	if err != nil {
		return plan, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&plan); err != nil {
		return plan, err
	}
	return plan, plan.Validate()
}
