// Package admin0 exposes the mock's control-plane endpoints under /admin0/*.
package admin0

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"

	"github.com/sergiught/auth0-mock/internal/matches"
)

// Mount registers the admin0 routes on r.
func Mount(r *httprouter.Router, store *matches.Store) {
	r.HandlerFunc(http.MethodPost, "/admin0/reset", reset(store))
	r.HandlerFunc(http.MethodGet, "/admin0/matches", list(store))
}

func reset(store *matches.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		store.ResetAll()
		w.WriteHeader(http.StatusNoContent)
	}
}

type listResponse struct {
	Matches []matches.Match `json:"matches"`
}

func list(store *matches.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listResponse{Matches: store.List()})
	}
}
