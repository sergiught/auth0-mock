package mgmtapi

import (
	"bytes"
	"io"
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
	// 0. Buffer the request body once: an HTTP body stream is read-once, and
	//    both spec validation and request-matcher comparison need it.
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "read body: "+err.Error(), "invalid_request")
			return
		}
		_ = r.Body.Close()
		body = b
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	// 1. Validate request against spec.
	if err := h.Validator.ValidateRequest(r, h.Op); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_request")
		return
	}
	// 2. Find a registered expectation for this request.
	exp := h.Store.Find(r.Method, r.URL.Path, h.Op.Template, matches.MatchableRequest{
		Query: r.URL.Query(),
		Body:  body,
	})
	if exp == nil {
		httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
			"no registered match for "+r.Method+" "+r.URL.Path, "no_match")
		return
	}
	// 3. Defense-in-depth response validation.
	if err := h.Validator.ValidateResponse(h.Op, exp.Response.Status, exp.Response.Headers, exp.Response.Body); err != nil {
		h.Log.Error().
			Str("op", h.Op.Op.OperationID).
			Int("status", exp.Response.Status).
			Err(err).
			Msg("registered match violates response schema")
		if h.Strict {
			httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
				"registered match violates schema: "+err.Error(), "invalid_match")
			return
		}
	}
	// 4. Write the registered response.
	for k, v := range exp.Response.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(exp.Response.Status)
	if _, err := w.Write(exp.Response.Body); err != nil {
		h.Log.Debug().
			Err(err).
			Str("op", h.Op.Op.OperationID).
			Msg("write failed (client likely gone)")
	}
}
