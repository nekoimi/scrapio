package v22_output_repo

import (
	"testing"

	"github.com/nekoimi/scrapio/internal/output"
)

func TestOutputInputBoundaries(t *testing.T) {
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "string"}}, UniqueKey: []string{"id"}}
	input := CheckInput{ExpectedRevision: 1, CaptureID: "7911a460-e239-49f2-bd9c-777d81682d91", StepID: "items", Stage: "list", Proposed: &TableInput{Name: "new", Schema: schema}, Mapping: []output.Mapping{{Source: "id", Target: "id"}}, UpdatePolicy: "update", EmptyPolicy: "preserve_existing"}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	input.TableID = "1"
	input.SchemaVersion = 1
	if input.Validate() == nil {
		t.Fatal("existing and proposed targets accepted together")
	}
	input.TableID = ""
	input.SchemaVersion = 0
	input.SampleID = input.CaptureID
	input.SampleRevision = 1
	if input.Validate() == nil {
		t.Fatal("both input sources accepted")
	}
	input.SampleID = ""
	input.SampleRevision = 0
	input.Mapping = nil
	if input.Validate() == nil {
		t.Fatal("missing mapping accepted")
	}
}
