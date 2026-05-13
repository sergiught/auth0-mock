// Package admin0 exposes the mock's control-plane endpoints under /admin0/*.
package admin0

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/matches"
)

// Mount registers the admin0 routes on r.
func Mount(r chi.Router, store *matches.Store) {
	r.Method(http.MethodPost, "/admin0/reset", &ResetHandler{Store: store})
	r.Method(http.MethodGet, "/admin0/matches", &ListHandler{Store: store})
}

// ResetHandler wipes all registered matches.
type ResetHandler struct {
	Store *matches.Store
}

func (h *ResetHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.Store.ResetAll()
	w.WriteHeader(http.StatusNoContent)
}

type listResponse struct {
	Matches []matches.Match `json:"matches"`
}

// ListHandler returns all registered matches as JSON.
type ListHandler struct {
	Store *matches.Store
}

func (h *ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, listResponse{Matches: h.Store.List()})
}
