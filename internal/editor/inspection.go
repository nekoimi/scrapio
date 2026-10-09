package editor

import (
	"errors"
	"math"
)

type Inspection struct {
	PageStateID         string    `json:"page_state_id"`
	Position            *Position `json:"position,omitempty"`
	ElementID           string    `json:"element_id,omitempty"`
	Mode                string    `json:"mode,omitempty"`
	Relation            string    `json:"relation,omitempty"`
	ChildIndex          int       `json:"child_index,omitempty"`
	ExpandSimilar       bool      `json:"expand_similar,omitempty"`
	Locator             *Locator  `json:"locator,omitempty"`
	Scope               *Locator  `json:"scope,omitempty"`
	Offset              int       `json:"offset,omitempty"`
	Limit               int       `json:"limit,omitempty"`
	PreviousFingerprint string    `json:"previous_fingerprint,omitempty"`
}

func (i *Inspection) Validate(operation string) error {
	if i.Limit == 0 {
		i.Limit = 50
	}
	if i.Mode == "" {
		i.Mode = "record"
	}
	if i.Relation == "" {
		i.Relation = "self"
	}
	if i.PageStateID == "" || len(i.PageStateID) > 256 || len(i.ElementID) > 128 || len(i.PreviousFingerprint) > 128 || i.Offset < 0 || i.Offset > 10000 || i.Limit < 1 || i.Limit > 50 || i.ChildIndex < 0 || i.ChildIndex > 10000 {
		return errors.New("page state, element reference or pagination is invalid")
	}
	if i.Mode != "record" && i.Mode != "field" || i.Relation != "self" && i.Relation != "parent" && i.Relation != "child" {
		return errors.New("invalid selection mode or relation")
	}
	for _, locator := range []*Locator{i.Locator, i.Scope} {
		if locator != nil && (locator.Strategy != "css" && locator.Strategy != "xpath" || locator.Expression == "" || len(locator.Expression) > 2048) {
			return errors.New("invalid locator")
		}
	}
	if i.Position != nil {
		for _, value := range []float64{i.Position.X, i.Position.Y} {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 8192 {
				return errors.New("invalid screenshot coordinate")
			}
		}
	}
	switch operation {
	case "inspect":
		if (i.Position == nil) == (i.ElementID == "") {
			return errors.New("provide either position or element_id")
		}
	case "dom":
	case "locator-check":
		if i.Locator == nil {
			return errors.New("locator is required")
		}
	default:
		return errors.New("unsupported inspection operation")
	}
	return nil
}
