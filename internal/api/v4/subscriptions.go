package v4

import (
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/co"
	"net/http"
)

func (a API) subscriptions(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	task := mux.Vars(r)["task_id"]
	if r.Method == "GET" {
		v, e := co.TaskSubscriptions(r.Context(), o, task)
		respond(w, r, v, e)
		return
	}
	var p co.SubscriptionRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.CreateSubscription(r.Context(), o, task, r.Header.Get("Idempotency-Key"), p)
	done(w, r, v, e, false)
}
func (a API) subscriptionControl(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.SubscriptionControl
	if !decode(w, r, &p) {
		return
	}
	v, e := co.ControlSubscription(r.Context(), o, mux.Vars(r)["task_id"], r.Header.Get("Idempotency-Key"), p)
	done(w, r, v, e, false)
}
