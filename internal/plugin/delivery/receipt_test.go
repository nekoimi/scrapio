package delivery

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/plugin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeliveryAria2FixedReceipt(t *testing.T) {
	task := plugin.Task{IdempotencyKey: "co.delivery:fixture", Input: map[string]any{"url": "https://example.test/file", "dir": "/downloads"}}
	var gid string
	submit, queries := 0, 0
	wrong, missing := false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Method string
			Params []json.RawMessage
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			return
		}
		var token string
		_ = json.Unmarshal(body.Params[0], &token)
		if token != "token:fixture-secret" {
			t.Error("credentials missing")
		}
		var result any
		switch body.Method {
		case "aria2.addUri":
			submit++
			var opts map[string]any
			_ = json.Unmarshal(body.Params[2], &opts)
			if opts["gid"] != gid || opts["dir"] != "/downloads" {
				t.Error("fixed options missing", opts)
			}
			result = gid
			if wrong {
				result = "other-gid"
			}
		case "aria2.tellStatus":
			queries++
			var id string
			_ = json.Unmarshal(body.Params[1], &id)
			if id != gid {
				t.Error("queried another request")
			}
			if missing {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 1, "message": "not found"}})
				return
			}
			result = map[string]any{"gid": id, "status": "waiting"}
		default:
			t.Error("unexpected method", body.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	}))
	defer srv.Close()
	p := NewAria2(config.Aria2Config{JsonRpc: srv.URL, Secret: "fixture-secret"})
	gid = p.DeliveryIdentity(task)
	if len(gid) != 16 || p.DeliveryIdentity(task) != gid {
		t.Fatal("unstable identity")
	}
	_, external, e := p.SubmitDelivery(context.Background(), task)
	if e != nil || external != gid {
		t.Fatal(external, e)
	}
	_, outcome, e := p.QueryDelivery(context.Background(), task, gid)
	if e != nil || outcome != "accepted" {
		t.Fatal(outcome, e)
	}
	wrong = true
	if _, _, e = p.SubmitDelivery(context.Background(), task); e == nil {
		t.Fatal("wrong GID accepted")
	}
	missing = true
	if _, outcome, e = p.QueryDelivery(context.Background(), task, gid); e == nil || outcome != "unknown" {
		t.Fatal("missing receipt treated as unsent")
	}
	before := queries
	if _, _, e = p.QueryDelivery(context.Background(), task, "other-gid"); e == nil || queries != before {
		t.Fatal("identity mismatch queried")
	}
	if submit != 2 || queries != 2 {
		t.Fatal(submit, queries)
	}
	p2 := NewAria2(config.Aria2Config{JsonRpc: srv.URL, Secret: "another-secret"})
	if p.DeliveryDestination() == p2.DeliveryDestination() {
		t.Fatal("destination did not freeze credentials")
	}
}
