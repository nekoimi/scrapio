package v22_query_repo

import "testing"

func TestDataSnapshotCursorAndExactViewEquality(t *testing.T) {
	id := "f4f9cc3e-0057-4c57-b47a-441793a328d0"
	got, offset, err := Cursor("q:" + id + ":50")
	if err != nil || got != id || offset != 50 {
		t.Fatal("cursor invalid", err)
	}
	for _, cursor := range []string{"q:bad:1", "q:" + id + ":-1", "q:" + id + ":10001", "q:" + id + ":x", "legacy-id"} {
		if _, _, err = Cursor(cursor); err == nil {
			t.Error("invalid cursor accepted", cursor)
		}
	}
	if equalJSON(`{"n":9007199254740992}`, `{"n":9007199254740993}`) {
		t.Fatal("large integers compared through float64")
	}
	if !equalJSON(`{"a":1,"b":2}`, `{"b":2,"a":1}`) {
		t.Fatal("JSON object ordering must not change retry equality")
	}
}
