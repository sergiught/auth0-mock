package mgmtapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/journal"
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
	if r.Body != nil && r.ContentLength != 0 {
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
		// Kin-openapi's err.Error() is multi-line with embedded
		// Schema/Value blocks — useful for the operator at debug
		// level, terrible for the wire response. ConciseValidationError
		// trims to "<field>: <reason>".
		h.Log.Debug().
			Str("op", h.Op.Op.OperationID).
			Err(err).
			Msg("request failed spec validation")
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			conciseValidationError(err), "invalid_request")
		return
	}
	// 2. Find a registered expectation for this request.
	req := matches.MatchableRequest{
		Query:   r.URL.Query(),
		Headers: r.Header,
		Body:    body,
	}
	exp := h.Store.Find(r.Method, r.URL.Path, h.Op.Template, req)
	if exp == nil {
		// TemplateOf runs under the store's read lock; Resolve never
		// calls back into the store.
		templateOf := func(method, path string) string {
			op, err := h.Validator.Resolve(method, path)
			if err != nil {
				return ""
			}
			return op.Template
		}
		nm := h.Store.NearMisses(r.Method, r.URL.Path, h.Op.Template, req, templateOf)
		journal.MarkNoMatch(r.Context(), nm)
		httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
			noMatchMessage(r.Method, r.URL.Path, h.Op.Template, nm), "no_match")
		return
	}
	h.respond(r.Context(), w, exp)
}

// noMatchMessage explains a no_match 404: which fields of the closest stub
// differed, or that nothing is registered for the operation.
func noMatchMessage(method, path, opTemplate string, nm []matches.NearMiss) string {
	msg := "no registered match for " + method + " " + path
	if len(nm) == 0 {
		return msg + "; no stubs registered for " + method + " " + opTemplate
	}
	const maxShown = 3
	diffs := nm[0].Differences
	fields := make([]string, 0, maxShown)
	for _, d := range diffs[:min(len(diffs), maxShown)] {
		fields = append(fields, d.Field)
	}
	msg += "; closest stub differed on " + strings.Join(fields, ", ")
	if len(diffs) > maxShown {
		msg += ", …"
	}
	return msg
}

// respond writes a matched expectation's canned response.
func (h *GenericHandler) respond(ctx context.Context, w http.ResponseWriter, exp *matches.Expectation) {
	journal.MarkMatched(ctx, exp.ID)
	res := exp.Served()
	// 3. Defense-in-depth response validation.
	if err := h.Validator.ValidateResponse(h.Op, res.Status, res.Headers, res.Body); err != nil {
		h.Log.Error().
			Str("op", h.Op.Op.OperationID).
			Int("status", res.Status).
			Err(err).
			Msg("registered match violates response schema")
		if h.Strict {
			httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
				"registered match violates schema: "+err.Error(), "invalid_match")
			return
		}
	}
	// 4. Write the registered response.
	for k, v := range res.Headers {
		// Vary is additive so the CORS middleware's Vary: Origin survives.
		if http.CanonicalHeaderKey(k) == "Vary" {
			w.Header().Add(k, v)
			continue
		}
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(res.Status)
	if _, err := w.Write(res.Body); err != nil { //nolint:gosec // res.Body is operator-supplied stub content registered via /admin0/expectations; faithfully echoing it is the entire point of the mock
		h.Log.Debug().
			Err(err).
			Str("op", h.Op.Op.OperationID).
			Msg("write failed (client likely gone)")
	}
}

// eventsHandler serves GET /events: a registered error stub when one
// matches the request, otherwise the live SSE stream. Registration
// refuses success stubs for this route, so a stub can only ever stand in
// for a failure — the stream stays the one source of 200s.
//
// Stubs are looked up before stream runs its own resume checks, so a
// stub can answer a request the stream would have accepted or refused
// differently — e.g. a 410 matched on a Last-Event-ID header.
type eventsHandler struct {
	stubs  *GenericHandler
	stream http.Handler
}

func (h *eventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	exp := h.stubs.Store.Find(r.Method, r.URL.Path, h.stubs.Op.Template, matches.MatchableRequest{
		Query:   r.URL.Query(),
		Headers: r.Header,
	})
	if exp == nil {
		h.stream.ServeHTTP(&markOn2xx{ResponseWriter: w, ctx: r.Context()}, r)
		return
	}
	h.stubs.respond(r.Context(), w, exp)
}

// markOn2xx marks the request matched only when the stream answers 2xx, so
// its own 4xx/410 refusals stay unmarked (recorded as rejected). The mark is
// made before the first write is delegated because the journal commits on it.
type markOn2xx struct {
	http.ResponseWriter
	ctx   context.Context
	wrote bool
}

func (m *markOn2xx) WriteHeader(code int) {
	if !m.wrote {
		m.wrote = true
		if code >= 200 && code < 300 {
			journal.MarkMatched(m.ctx, "")
		}
	}
	m.ResponseWriter.WriteHeader(code)
}

func (m *markOn2xx) Write(b []byte) (int, error) {
	if !m.wrote {
		m.WriteHeader(http.StatusOK)
	}
	return m.ResponseWriter.Write(b)
}

func (m *markOn2xx) Flush() {
	if !m.wrote {
		m.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(m.ResponseWriter).Flush()
}

func (m *markOn2xx) Unwrap() http.ResponseWriter { return m.ResponseWriter }
