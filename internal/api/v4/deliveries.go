package v4

import (
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/co"
	"net/http"
)

func (a API) deliveryCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := owner(w, r); !ok {
		return
	}
	write(w, r, 200, co.DeliveryCapabilities())
}
func (a API) deliveryControl(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.DeliveryControlRequest
	if !decode(w, r, &p) {
		return
	}
	o, e := co.ControlDelivery(r.Context(), owner, mux.Vars(r)["run_id"], r.Header.Get("Idempotency-Key"), p)
	done(w, r, o, e, false)
}
