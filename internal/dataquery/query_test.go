package dataquery

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/output"
)

func schema() output.Schema {
	return output.Schema{Fields: []output.Field{{Key: "title", Name: "Title", Type: "string"}, {Key: "n", Name: "Number", Type: "integer"}, {Key: "tags", Name: "Tags", Type: "string", Multiple: true}, {Key: "date", Name: "Date", Type: "date"}}, UniqueKey: []string{"n"}}
}
func TestDataQueryExactNumbersAndBoundSQL(t *testing.T) {
	q, err := Decode(`{"filters":[{"field":"n","op":"gte","value":"9007199254740993"},{"field":"title","op":"contains","value":"x%' OR true --"}],"sort":{"field":"n","direction":"asc"},"columns":["n","title"]}`)
	if err != nil {
		t.Fatal(err)
	}
	q, err = Normalize(q, schema())
	if err != nil {
		t.Fatal(err)
	}
	if string(q.Filters[0].Value) != `"9007199254740993"` {
		t.Fatal("filter numeric value would be rounded by browser", string(q.Filters[0].Value))
	}
	sql, args, order, orderArgs := SQL(q, schema())
	if strings.Contains(sql, "OR true") || strings.Contains(sql, "900719") || args[2] != "9007199254740993" || !strings.Contains(order, "::numeric") || len(orderArgs) != 2 {
		t.Fatalf("unsafe or imprecise SQL %s %+v %s", sql, args, order)
	}
	if strings.Count(sql, "?") != len(args) || strings.Count(order, "?") != len(orderArgs) {
		t.Fatal("SQL placeholder mismatch")
	}
}
func TestDataQueryRejectsInvalidTypesAndUnknownFields(t *testing.T) {
	for _, raw := range []string{`{"filters":[{"field":"missing","op":"eq","value":"x"}]}`, `{"filters":[{"field":"n","op":"contains","value":"x"}]}`, `{"filters":[{"field":"tags","op":"eq","value":"x"}]}`, `{"filters":[{"field":"n","op":"eq","value":"1.5"}]}`, `{"sort":{"field":"title","direction":"DROP TABLE"}}`, `{"columns":["n","n"]}`} {
		q, err := Decode(raw)
		if err == nil {
			_, err = Normalize(q, schema())
		}
		if err == nil {
			t.Error("accepted invalid query", raw)
		}
	}
	for _, raw := range []string{`{"unknown":1}`, `{} {}`, `[]`, `null`} {
		if _, err := Decode(raw); err == nil {
			t.Error("accepted invalid body", raw)
		}
	}
	q, err := Normalize(Query{}, schema())
	if err != nil || q.Sort.Field != "@first_observed_at" || len(q.Columns) != 4 {
		t.Fatal("default query invalid", err)
	}
	q.Filters = []Filter{{Field: "tags", Op: "empty"}}
	q, err = Normalize(q, schema())
	if err != nil {
		t.Fatal(err)
	}
	sql, args, _, _ := SQL(q, schema())
	if strings.Count(sql, "?") != len(args) {
		t.Fatal("empty predicate placeholder mismatch")
	}
}
func TestDataExportPrecisionAndCSVFormulaText(t *testing.T) {
	q, _ := Normalize(Query{Columns: []string{"n", "title"}}, schema())
	rows := []map[string]string{{"record_id": "id", "revision": "2", "schema_version": "1", "values_json": `{"n":9007199254740993,"title":" =HYPERLINK(\"evil\")"}`}}
	check := func() error { return nil }
	raw, err := Render("json", schema(), q, rows, check)
	if err != nil || !strings.Contains(string(raw), `9007199254740993`) {
		t.Fatal("JSON integer rounded", err, string(raw))
	}
	var content []map[string]json.RawMessage
	if json.Unmarshal(raw, &content) != nil || !strings.Contains(string(content[0]["values"]), "9007199254740993") {
		t.Fatal("invalid JSON export")
	}
	raw, err = Render("csv", schema(), q, rows, check)
	if err != nil {
		t.Fatal(err)
	}
	cells, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff"))).ReadAll()
	if err != nil || cells[1][3] != "9007199254740993" || !strings.HasPrefix(cells[1][4], "'") {
		t.Fatal("CSV safety or precision lost", err, cells)
	}
	raw, err = Render("csv", schema(), q, nil, check)
	if err != nil || !strings.Contains(string(raw), "Number") {
		t.Fatal("empty CSV must retain header", err)
	}
	if _, err = Render("json", schema(), q, rows, func() error { return context.Canceled }); err != context.Canceled {
		t.Fatal("cancellation ignored")
	}
	for _, value := range []string{"=1", "+1", "-1", "@cmd", "\ttext", "\rtext", "  =1"} {
		if CSVText(value) == value {
			t.Error("formula text not neutralized", value)
		}
	}
}

func TestDataExportMetadataDoesNotOverwriteBusinessFields(t *testing.T) {
	s := schema()
	s.Fields = append(s.Fields, output.Field{Key: "record_id", Name: "Record_ID", Type: "string"})
	q, err := Normalize(Query{Columns: []string{"record_id"}}, s)
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]string{{"record_id": "actual-id", "revision": "1", "schema_version": "1", "values_json": `{"record_id":"business-id"}`}}
	raw, err := Render("json", s, q, rows, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var result []struct {
		RecordID string            `json:"record_id"`
		Values   map[string]string `json:"values"`
	}
	if json.Unmarshal(raw, &result) != nil || result[0].RecordID != "actual-id" || result[0].Values["record_id"] != "business-id" {
		t.Fatal("business field overwritten", string(raw))
	}
	if _, err = Render("json", s, q, make([]map[string]string, MaxRows+1), func() error { return nil }); err == nil {
		t.Fatal("row limit ignored")
	}
}
