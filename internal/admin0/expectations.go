package admin0

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// PostExpectationHandler registers (upserts) an expectation for the Management
// API operation identified by {method, path}.
type PostExpectationHandler struct {
	Store     *matches.Store
	Validator *spec.Validator
}

func (h *PostExpectationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var p matches.Registration
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	exp, err := matches.Prepare(h.Validator, p)
	if err != nil {
		var re *matches.RegistrationError
		if errors.As(err, &re) {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", re.Message, re.Code)
			return
		}
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_body")
		return
	}
	stored := h.Store.Put(exp)
	// Echo the assigned ID so the SDK / curl caller can hold a handle
	// to this specific registration (used by per-stub Hits / Clear in
	// batches 6b and 6d). Status changes 204 → 201 to advertise the
	// new resource.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		ID string `json:"id"`
	}{ID: stored.ID})
}

type listExpectationsResponse struct {
	Expectations []matches.Expectation `json:"expectations"`
}

// ListExpectationsHandler returns every registered expectation.
type ListExpectationsHandler struct {
	Store *matches.Store
}

func (h *ListExpectationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, listExpectationsResponse{Expectations: h.Store.List()})
}

// deleteExpectationBody optionally narrows a DELETE to one operation.
type deleteExpectationBody struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// DeleteExpectationsHandler clears expectations. An empty body clears all; a
// {method, path} body clears every expectation registered for that operation
// (the catch-all and every request-matched one).
//
// Two intentional behaviours worth noting:
//   - An empty/whitespace-only body means "clear all". The read error from
//     io.ReadAll is deliberately ignored: a failed or empty read falls through
//     to ResetAll, which is a benign outcome for a teardown DELETE.
//   - Clearing an operation that was never registered is an idempotent no-op
//     (returns 204). ResetEndpoint is documented as a no-op for unregistered
//     keys, and DELETE intentionally does NOT validate {method, path} against
//     the spec (unlike POST) because teardown should be forgiving.
type DeleteExpectationsHandler struct {
	Store *matches.Store
}

func (h *DeleteExpectationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if len(bytes.TrimSpace(raw)) == 0 {
		h.Store.ResetAll()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var p deleteExpectationBody
	if err := json.Unmarshal(raw, &p); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	p.Method = strings.ToUpper(p.Method)
	if p.Method == "" || p.Path == "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"method and path are required to clear one operation's expectations", "invalid_body")
		return
	}
	h.Store.ResetEndpoint(p.Method, p.Path, matches.KindOf(p.Path))
	w.WriteHeader(http.StatusNoContent)
}

// DeleteExpectationByIDHandler removes a single expectation by its
// store-assigned ID. The route is /admin0/expectations/{id}; chi
// extracts the id parameter. Idempotent — deleting an unknown id is
// a 204 no-op, same as the bulk DELETE on an unregistered operation.
type DeleteExpectationByIDHandler struct {
	Store *matches.Store
}

func (h *DeleteExpectationByIDHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"expectation id is required", "invalid_body")
		return
	}
	h.Store.DeleteByID(id)
	w.WriteHeader(http.StatusNoContent)
}

// GetExpectationByIDHandler returns a single expectation by its
// store-assigned ID with its current Hits counter populated. Used by
// the SDK's RegisteredExpectation.Hits(ctx). 404 (unknown_id) when no
// such expectation exists — distinguishes "cleared" from "never hit"
// for the caller.
type GetExpectationByIDHandler struct {
	Store *matches.Store
}

func (h *GetExpectationByIDHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"expectation id is required", "invalid_body")
		return
	}
	exp := h.Store.GetByID(id)
	if exp == nil {
		httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
			"no expectation with id "+id, "unknown_id")
		return
	}
	render.JSON(w, r, exp)
}
