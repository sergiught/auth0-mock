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
// operation (method + path) plus the canned response to return for it.
type expectationBody struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// PostExpectationHandler registers (upserts) a canned response for the
// Management API operation identified by {method, path}.
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
	if p.Status == 0 {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "status is required", "invalid_body")
		return
	}
	op, err := h.Validator.Resolve(p.Method, p.Path)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"no Management API operation for "+p.Method+" "+p.Path, "unknown_operation")
		return
	}
	if err := h.Validator.ValidateRegistration(op, p.Status, p.Body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"registered response violates schema for status "+strconv.Itoa(p.Status)+": "+err.Error(),
			"invalid_match")
		return
	}
	storePath := p.Path
	if matches.KindOf(p.Path) == matches.KindTemplate {
		storePath = op.Template
	}
	h.Store.Put(matches.Match{
		Method:  p.Method,
		Path:    storePath,
		Kind:    matches.KindOf(p.Path),
		Status:  p.Status,
		Headers: p.Headers,
		Body:    p.Body,
	})
	w.WriteHeader(http.StatusNoContent)
}

type listExpectationsResponse struct {
	Expectations []matches.Match `json:"expectations"`
}

// ListExpectationsHandler returns every registered expectation.
type ListExpectationsHandler struct {
	Store *matches.Store
}

func (h *ListExpectationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, listExpectationsResponse{Expectations: h.Store.List()})
}

// deleteExpectationBody optionally narrows a DELETE to a single expectation.
type deleteExpectationBody struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// DeleteExpectationsHandler clears expectations. An empty body clears all; a
// {method, path} body clears just that one.
//
// Two intentional behaviours worth noting:
//   - An empty/whitespace-only body means "clear all". The read error from
//     io.ReadAll is deliberately ignored: a failed or empty read falls through
//     to ResetAll, which is a benign outcome for a teardown DELETE.
//   - Clearing an expectation that was never registered is an idempotent no-op
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
			"method and path are required to clear one expectation", "invalid_body")
		return
	}
	h.Store.ResetEndpoint(p.Method, p.Path, matches.KindOf(p.Path))
	w.WriteHeader(http.StatusNoContent)
}
