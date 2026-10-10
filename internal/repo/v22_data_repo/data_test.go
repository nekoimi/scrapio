package v22_data_repo

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/capture"
	"strings"
	"testing"
)

func TestEvidenceChunk(t *testing.T) {
	raw := "<script>alert(1)</script>电影😀汉字"
	hash := capture.Hash([]byte(raw))
	offset := 0
	parts := ""
	for offset < len(raw) {
		a, err := Chunk(raw, hash, offset, 5)
		if err != nil || a.Status != "available" || a.Next <= offset {
			t.Fatalf("%+v %v", a, err)
		}
		parts += a.Content
		offset = a.Next
	}
	if parts != raw {
		t.Fatal("chunk corrupts text")
	}
	if a, e := Chunk(raw, "wrong", 0, 20); e != nil || a.Status != "corrupt" || a.Content != "" {
		t.Fatal("corrupt evidence exposed")
	}
	if a, e := Chunk("", hash, 0, 20); e != nil || a.Status != "unavailable" {
		t.Fatal("missing evidence fabricated")
	}
	if _, e := Chunk(raw, hash, len(raw)-1, 20); e == nil {
		t.Fatal("mid-rune offset accepted")
	}
	for _, args := range [][2]int{{-1, 10}, {0, 3}, {0, 65537}, {len(raw) + 1, 10}} {
		if _, e := Chunk(raw, hash, args[0], args[1]); e == nil {
			t.Fatal("invalid chunk arguments")
		}
	}
}
func TestRecordPrecisionAndHistoricalFields(t *testing.T) {
	fields, e := ValueFields(`{"integer":900719925474099312345,"title":"旧标题"}`, `{"fields":[{"field_key":"integer","name":"历史数值","type":"integer"},{"field_key":"title","name":"历史名称","type":"string"}]}`)
	if e != nil || fields[0]["value_json"] != "900719925474099312345" || fields[1]["name"] != "历史名称" {
		t.Fatalf("%+v %v", fields, e)
	}
	raw, _ := json.Marshal(fields)
	if !strings.Contains(string(raw), `"value_json":"900719925474099312345"`) {
		t.Fatal("precision lost")
	}
}
func TestReadPageCursor(t *testing.T) {
	rows := []Row{{"record_id": "a"}, {"record_id": "b"}, {"record_id": "c"}}
	p := paged(rows, 2, "record_id")
	if !p.More || p.Next != "b" || len(p.Items) != 2 {
		t.Fatal(p)
	}
	if p := paged(rows, 3, "record_id"); p.More || p.Next != "" {
		t.Fatal(p)
	}
}
