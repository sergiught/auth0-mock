package admin0

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// expectationBody is the POST /admin0/expectations request payload: the target
// operation (method + path), an optional request matcher, and the canned
// response to return when the matcher applies, plus optional later answers.
type expectationBody struct {
	Method   string                  `json:"method"`
	Path     string                  `json:"path"`
	Request  *matches.RequestMatcher `json:"request,omitempty"`
	Response matches.ResponseDef     `json:"response"`
	Then     []matches.ResponseDef   `json:"then,omitempty"`
}

// PostExpectationHandler registers (upserts) an expectation for the Management
// API operation identified by {method, path}.
type PostExpectationHandler struct {
	Store     *matches.Store
	Validator *spec.Validator
}

func (h *PostExpectationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var p expectationBody
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	p.Method = strings.ToUpper(p.Method)
	if p.Method == "" || p.Path == "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "method and path are required", "invalid_body")
		return
	}
	if p.Response.Status == 0 {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "response.status is required", "invalid_body")
		return
	}
	op, err := h.Validator.Resolve(p.Method, p.Path)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"no Management API operation for "+p.Method+" "+p.Path, "unknown_operation")
		return
	}
	// The event stream's success answer is the live stream itself, fed by
	// POST /admin0/events. A canned 2xx would silently replace it with a
	// static body, so only error stubs may stand in for it.
	if op.Op.OperationID == spec.SubscribeEventsOperationID && p.Response.Status < http.StatusBadRequest {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"GET /api/v2/events accepts only error stubs (status >= 400); push events with POST /admin0/events instead",
			"invalid_match")
		return
	}
	if err := h.Validator.ValidateRegistration(op, p.Response.Status, p.Response.Body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"registered response violates schema for status "+strconv.Itoa(p.Response.Status)+": "+err.Error(),
			"invalid_match")
		return
	}
	if msg, code := h.validateThen(op, p.Then); msg != "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", msg, code)
		return
	}
	// Normalize an empty request matcher to a nil catch-all so a catch-all has
	// exactly one representation in the store.
	req := p.Request
	if req.IsEmpty() {
		req = nil
	}
	if req != nil {
		if err := h.Validator.ValidateRequestMatcher(op, req.Body); err != nil {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				"request matcher body violates request schema: "+err.Error(), "invalid_request_match")
			return
		}
		if err := h.Validator.ValidateQueryMatcher(op, req.Query); err != nil {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				"request matcher query is invalid: "+err.Error(), "invalid_request_match")
			return
		}
	}
	kind := matches.KindOf(p.Path)
	storePath := p.Path
	if kind == matches.KindTemplate {
		storePath = op.Template
	}
	stored := h.Store.Put(matches.Expectation{
		Method:   p.Method,
		Path:     storePath,
		Kind:     kind,
		Request:  req,
		Response: p.Response,
		Then:     p.Then,
	})
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

// validateThen checks every sequence entry like the primary response and
// returns the first problem as a message naming the entry plus its error
// code, or "" when valid.
func (h *PostExpectationHandler) validateThen(op spec.Operation, then []matches.ResponseDef) (msg, code string) {
	for i, t := range then {
		at := "then[" + strconv.Itoa(i) + "]"
		switch {
		case t.Status == 0:
			return at + ".status is required", "invalid_body"
		case op.Op.OperationID == spec.SubscribeEventsOperationID && t.Status < http.StatusBadRequest:
			return "GET /api/v2/events accepts only error stubs (status >= 400); " + at + " has status " + strconv.Itoa(t.Status), "invalid_match"
		}
		if err := h.Validator.ValidateRegistration(op, t.Status, t.Body); err != nil {
			return "registered " + at + " violates schema for status " + strconv.Itoa(t.Status) + ": " + err.Error(), "invalid_match"
		}
	}
	return "", ""
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
