package v4

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/ai"
	"github.com/nekoimi/scrapio/internal/co"
	"github.com/nekoimi/scrapio/internal/config"
)

func (a API) aiConfig() *config.AIAssistConfig {
	if a.Config == nil {
		return nil
	}
	return a.Config.AIAssist
}
func (a API) mountAI(v *mux.Router) {
	v.HandleFunc("/ai-capabilities", a.aiCapabilities).Methods("GET")
	v.HandleFunc("/ai/prepare", a.aiPrepare).Methods("POST")
	v.HandleFunc("/ai/suggestions", a.aiList).Methods("GET")
	v.HandleFunc("/ai/suggestions/{suggestion_id}", a.aiSuggestion).Methods("GET")
	v.HandleFunc("/ai/suggestions/{suggestion_id}", a.aiDelete).Methods("DELETE")
	v.HandleFunc("/ai/suggestions/{suggestion_id}/call", a.aiCall).Methods("POST")
	v.HandleFunc("/ai/suggestions/{suggestion_id}/{action:validate|accept|apply|reject}", a.aiControl).Methods("POST")
}

func (a API) aiDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.DeleteAI(r.Context(), owner, mux.Vars(r)["suggestion_id"], r.Header.Get("Idempotency-Key"), p.Confirmed)
	done(w, r, o, e, false)
}
func (a API) aiCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := owner(w, r); !ok {
		return
	}
	write(w, r, 200, co.AICapabilities(a.aiConfig()))
}
func (a API) aiPrepare(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.AIPrepareRequest
	if !decode(w, r, &p) {
		return
	}
	o, e := co.PrepareAI(r.Context(), owner, r.Header.Get("Idempotency-Key"), p, a.aiConfig())
	done(w, r, o, e, false)
}
func (a API) aiList(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, e := co.AIList(r.Context(), owner, q.Get("resource_kind"), q.Get("resource_id"))
	respond(w, r, v, e)
}
func (a API) aiSuggestion(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	v, e := co.AIView(r.Context(), owner, mux.Vars(r)["suggestion_id"])
	respond(w, r, v, e)
}
func (a API) aiCall(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.AIControlRequest
	if !decode(w, r, &p) {
		return
	}
	o, e := co.CallAI(r.Context(), owner, mux.Vars(r)["suggestion_id"], r.Header.Get("Idempotency-Key"), p, a.aiConfig(), ai.AssistClient{})
	done(w, r, o, e, false)
}
func (a API) aiControl(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.AIControlRequest
	if !decode(w, r, &p) {
		return
	}
	vars := mux.Vars(r)
	o, e := co.AIControl(r.Context(), owner, vars["suggestion_id"], r.Header.Get("Idempotency-Key"), vars["action"], p, a.aiConfig())
	done(w, r, o, e, false)
}
