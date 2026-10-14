package v4

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"net/http"
)

func (a API) transformPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := owner(w, r); !ok {
		return
	}
	var p struct {
		Transform *collector.Transform `json:"transform"`
		Value     json.RawMessage      `json:"value"`
		Target    collector.Field      `json:"target"`
	}
	if !decode(w, r, &p) {
		return
	}
	var value any
	if len(p.Value) == 0 {
		fail(w, r, collector.Invalid("value", "需要显式样例值，可为null"))
		return
	}
	if e := collector.Strict(p.Value, &value); e != nil {
		fail(w, r, e)
		return
	}
	v, e := collector.PreviewTransform(r.Context(), p.Transform, value, p.Target)
	respond(w, r, v, e)
}
