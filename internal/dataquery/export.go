package dataquery

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/nekoimi/scrapio/internal/output"
)

func CSVText(value string) string {
	probe := strings.TrimLeftFunc(value, unicode.IsSpace)
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || len(probe) > 0 && strings.ContainsRune("=+-@", rune(probe[0])) {
		return "'" + value
	}
	return value
}

// RawMessage keeps JSON numeric values exact, including integers larger than JavaScript's safe range.
func Render(format string, schema output.Schema, q Query, rows []map[string]string, check func() error) ([]byte, error) {
	if len(rows) > MaxRows {
		return nil, errors.New("export exceeds 10000 rows")
	}
	var buf bytes.Buffer
	if format == "csv" {
		buf.Write([]byte{0xef, 0xbb, 0xbf})
		writer := csv.NewWriter(&buf)
		names := map[string]string{}
		for _, f := range schema.Fields {
			names[f.Key] = f.Name
		}
		header := []string{"Scrapio.record_id", "Scrapio.revision", "Scrapio.schema_version"}
		for _, key := range q.Columns {
			header = append(header, CSVText(names[key]))
		}
		if err := writer.Write(header); err != nil {
			return nil, err
		}
		writer.Flush()
		if writer.Error() != nil {
			return nil, writer.Error()
		}
		for _, row := range rows {
			if err := check(); err != nil {
				return nil, err
			}
			var values map[string]json.RawMessage
			if err := json.Unmarshal([]byte(row["values_json"]), &values); err != nil {
				return nil, err
			}
			cells := []string{row["record_id"], row["revision"], row["schema_version"]}
			for _, key := range q.Columns {
				raw := values[key]
				value := string(raw)
				if len(raw) > 0 && raw[0] == '"' {
					if err := json.Unmarshal(raw, &value); err != nil {
						return nil, err
					}
					value = CSVText(value)
				}
				if value == "null" {
					value = ""
				}
				cells = append(cells, value)
			}
			if err := writer.Write(cells); err != nil {
				return nil, err
			}
			writer.Flush()
			if writer.Error() != nil {
				return nil, writer.Error()
			}
			if buf.Len() > MaxBytes {
				return nil, errors.New("export exceeds 16 MiB")
			}
		}
	} else if format == "json" {
		buf.WriteByte('[')
		for index, row := range rows {
			if err := check(); err != nil {
				return nil, err
			}
			var values map[string]json.RawMessage
			if err := json.Unmarshal([]byte(row["values_json"]), &values); err != nil {
				return nil, err
			}
			selected := map[string]json.RawMessage{}
			for _, key := range q.Columns {
				selected[key] = values[key]
				if len(selected[key]) == 0 {
					selected[key] = json.RawMessage("null")
				}
			}
			raw, err := json.Marshal(struct {
				RecordID      string                     `json:"record_id"`
				Revision      string                     `json:"revision"`
				SchemaVersion string                     `json:"schema_version"`
				Values        map[string]json.RawMessage `json:"values"`
			}{row["record_id"], row["revision"], row["schema_version"], selected})
			if err != nil {
				return nil, err
			}
			if index > 0 {
				buf.WriteByte(',')
			}
			buf.Write(raw)
			if buf.Len() > MaxBytes-1 {
				return nil, errors.New("export exceeds 16 MiB")
			}
		}
		buf.WriteByte(']')
	} else {
		return nil, errors.New("format must be csv or json")
	}
	return buf.Bytes(), nil
}
