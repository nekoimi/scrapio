package v22_run_repo

import (
	"encoding/json"
	"testing"

	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/publication"
)

func TestFormalVersionPlanRejectsChangedDefinitionAndSchema(t *testing.T) {
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "integer"}}, UniqueKey: []string{"id"}}
	raw, _ := json.Marshal(schema)
	config := output.Config{TableID: "1", SchemaVersion: 1, SchemaHash: output.SchemaHash(schema), StepID: "items", Stage: "list", Mapping: []output.Mapping{{Source: "id", Target: "id"}}, UpdatePolicy: "update", EmptyPolicy: "preserve_existing", CheckID: "be0aa19c-8bb3-4da4-9a91-b8690c0b6dde"}
	definition, _ := json.Marshal(map[string]any{"definition_version": 1, "entry_url": "https://example.com", "http_request": map[string]any{"method": "GET"}, "steps": []any{map[string]any{"step_id": "items", "type": "json_records", "config": map[string]any{"array_pointer": "", "max_records": 20, "fields": []any{map[string]any{"field_key": "id", "name": "id", "pointer": "/id", "type": "integer", "required": true}}}}}, "output": config})
	v := &table.V22Version{Definition: string(definition), DefinitionHash: publication.DefinitionHash(string(definition)), EntryType: "json", OutputSchema: string(raw), ContractVersion: publication.ContractVersion, InterpreterVersion: extraction.InterpreterVersion}
	if _, _, err := VersionPlan(v); err != nil {
		t.Fatal(err)
	}
	v.DefinitionHash = "old"
	if _, _, err := VersionPlan(v); err == nil {
		t.Fatal("changed version accepted")
	}
	v.DefinitionHash = publication.DefinitionHash(v.Definition)
	v.OutputSchema = `{"fields":[{"field_key":"id","name":"id","type":"string"}],"unique_key":["id"]}`
	if _, _, err := VersionPlan(v); err == nil {
		t.Fatal("changed Schema accepted")
	}
}
