package workflow_repo

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
)

type FieldChange struct {
	Candidate int    `json:"candidate"`
	Field     string `json:"field"`
	Left      any    `json:"left,omitempty"`
	Right     any    `json:"right,omitempty"`
}

type SampleComparison struct {
	SampleID int64         `json:"sample_id"`
	PageRole string        `json:"page_role"`
	Changed  bool          `json:"changed"`
	Fields   []FieldChange `json:"fields"`
	Left     SamplePreview `json:"left"`
	Right    SamplePreview `json:"right"`
}

type VersionSampleComparison struct {
	LeftVersionID   int64              `json:"left_version_id"`
	RightVersionID  int64              `json:"right_version_id"`
	SampleVersionID int64              `json:"sample_version_id"`
	Changed         int                `json:"changed"`
	Samples         []SampleComparison `json:"samples"`
}

// CompareVersionSamples replays the same immutable sample set under both
// definitions. The reported save decisions are dry-run estimates only.
func CompareVersionSamples(leftID, rightID, sampleVersionID int64) (VersionSampleComparison, error) {
	result := VersionSampleComparison{LeftVersionID: leftID, RightVersionID: rightID, SampleVersionID: sampleVersionID, Samples: []SampleComparison{}}
	left, has, err := GetVersion(leftID)
	if err != nil {
		return result, err
	}
	if !has {
		return result, errors.New("left version not found")
	}
	right, has, err := GetVersion(rightID)
	if err != nil {
		return result, err
	}
	if !has {
		return result, errors.New("right version not found")
	}
	source, has, err := GetVersion(sampleVersionID)
	if err != nil {
		return result, err
	}
	if !has {
		return result, errors.New("sample version not found")
	}
	if left.WorkflowId != right.WorkflowId || source.WorkflowId != left.WorkflowId {
		return result, errors.New("versions must belong to the same workflow")
	}
	var samples []table.WorkflowSample
	if err := db.Instance().Where("workflow_version_id=?", sampleVersionID).Asc("id").Limit(51).Find(&samples); err != nil {
		return result, err
	}
	if len(samples) == 0 || len(samples) > 50 {
		return result, errors.New("comparison requires 1–50 samples")
	}
	for _, sample := range samples {
		leftPreview, err := PreviewSampleForComparison(leftID, &sample)
		if err != nil {
			return result, fmt.Errorf("sample %d left: %w", sample.Id, err)
		}
		rightPreview, err := PreviewSampleForComparison(rightID, &sample)
		if err != nil {
			return result, fmt.Errorf("sample %d right: %w", sample.Id, err)
		}
		item := SampleComparison{SampleID: sample.Id, PageRole: sample.PageRole, Left: leftPreview, Right: rightPreview, Fields: changedPreviewFields(leftPreview, rightPreview)}
		item.Changed = len(item.Fields) > 0 || leftPreview.ActualOutcome != rightPreview.ActualOutcome || leftPreview.ActualError != rightPreview.ActualError || !reflect.DeepEqual(leftPreview.Decisions, rightPreview.Decisions) || !reflect.DeepEqual(leftPreview.Discovered, rightPreview.Discovered) || leftPreview.NextURL != rightPreview.NextURL
		if item.Changed {
			result.Changed++
		}
		result.Samples = append(result.Samples, item)
	}
	return result, nil
}

type fieldKey struct {
	candidate int
	name      string
}

func previewFields(preview SamplePreview) map[fieldKey]any {
	out := map[fieldKey]any{}
	for _, step := range preview.Steps {
		for _, field := range step.Fields {
			out[fieldKey{step.Candidate, field.Name + ".raw"}] = field.Raw
			out[fieldKey{step.Candidate, field.Name + ".matches"}] = field.MatchCount
			out[fieldKey{step.Candidate, field.Name + ".converted"}] = field.Converted
			out[fieldKey{step.Candidate, field.Name + ".error"}] = field.Error
		}
		for name, value := range step.Values {
			out[fieldKey{step.Candidate, name}] = value
		}
	}
	return out
}

func changedPreviewFields(left, right SamplePreview) []FieldChange {
	lv, rv := previewFields(left), previewFields(right)
	keys := map[fieldKey]bool{}
	for key := range lv {
		keys[key] = true
	}
	for key := range rv {
		keys[key] = true
	}
	ordered := make([]fieldKey, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].candidate != ordered[j].candidate {
			return ordered[i].candidate < ordered[j].candidate
		}
		return ordered[i].name < ordered[j].name
	})
	out := make([]FieldChange, 0)
	for _, key := range ordered {
		if !reflect.DeepEqual(lv[key], rv[key]) {
			out = append(out, FieldChange{Candidate: key.candidate, Field: key.name, Left: lv[key], Right: rv[key]})
		}
	}
	return out
}
