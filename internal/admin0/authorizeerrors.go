package admin0

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/httperr"
)

// PostAuthorizeErrorHandler registers a rule that makes /authorize fail.
type PostAuthorizeErrorHandler struct {
	Store *authorizeerr.Store
}

func (h *PostAuthorizeErrorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var rule authorizeerr.Rule
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rule); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	if err := authorizeerr.Validate(rule); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_body")
		return
	}
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, h.Store.Add(rule))
}

// ListAuthorizeErrorsHandler returns every rule in registration order.
type ListAuthorizeErrorsHandler struct {
	Store *authorizeerr.Store
}

func (h *ListAuthorizeErrorsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, struct {
		Rules []authorizeerr.Rule `json:"rules"`
	}{Rules: h.Store.List()})
}

// DeleteAuthorizeErrorsHandler removes every rule.
type DeleteAuthorizeErrorsHandler struct {
	Store *authorizeerr.Store
}

func (h *DeleteAuthorizeErrorsHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.Store.Clear()
	w.WriteHeader(http.StatusNoContent)
}

// DeleteAuthorizeErrorByIDHandler removes one rule; an unknown ID is a no-op.
type DeleteAuthorizeErrorByIDHandler struct {
	Store *authorizeerr.Store
}

func (h *DeleteAuthorizeErrorByIDHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Store.Delete(chi.URLParam(r, "id"))
	w.WriteHeader(http.StatusNoContent)
}
