package mgmtapi

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// GenericHandler is the main spec-driven handler for a Management API operation.
type GenericHandler struct {
	Op        spec.Operation
	Validator *spec.Validator
	Store     *matches.Store
	Log       zerolog.Logger
	Strict    bool
}

func (h *GenericHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Validate request against spec.
	if err := h.Validator.ValidateRequest(r, h.Op); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_request")
		return
	}
	// 2. Find a registered match.
	m := h.Store.Find(r.Method, r.URL.Path, h.Op.Template)
	if m == nil {
		httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
			"no registered match for "+r.Method+" "+r.URL.Path, "no_match")
		return
	}
	// 3. Defense-in-depth response validation.
	if err := h.Validator.ValidateResponse(h.Op, m.Status, m.Headers, m.Body); err != nil {
		h.Log.Error().
			Str("op", h.Op.Op.OperationID).
			Int("status", m.Status).
			Err(err).
			Msg("registered match violates response schema")
		if h.Strict {
			httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
				"registered match violates schema: "+err.Error(), "invalid_match")
			return
		}
	}
	// 4. Write the registered response.
	for k, v := range m.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(m.Status)
	_, _ = w.Write(m.Body)
}
