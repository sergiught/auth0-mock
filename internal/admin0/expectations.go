package admin0

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// expectationBody is the POST /admin0/expectations request payload: the target
// operation (method + path), an optional request matcher, and the canned
// response to return when the matcher applies.
type expectationBody struct {
	Method   string                  `json:"method"`
	Path     string                  `json:"path"`
	Request  *matches.RequestMatcher `json:"request,omitempty"`
	Response matches.ResponseDef     `json:"response"`
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
	if err := h.Validator.ValidateRegistration(op, p.Response.Status, p.Response.Body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"registered response violates schema for status "+strconv.Itoa(p.Response.Status)+": "+err.Error(),
			"invalid_match")
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
	storePath := p.Path
	if matches.KindOf(p.Path) == matches.KindTemplate {
		storePath = op.Template
	}
	h.Store.Put(matches.Expectation{
		Method:   p.Method,
		Path:     storePath,
		Kind:     matches.KindOf(p.Path),
		Request:  req,
		Response: p.Response,
	})
	w.WriteHeader(http.StatusNoContent)
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
