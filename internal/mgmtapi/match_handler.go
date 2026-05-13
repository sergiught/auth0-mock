package mgmtapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

type matchPayload struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// MatchHandler registers a canned response for a Management API operation.
type MatchHandler struct {
	Op        spec.Operation
	Validator *spec.Validator
	Store     *matches.Store
}

func (h *MatchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "read body: "+err.Error(), "invalid_body")
		return
	}
	var p matchPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	if p.Status == 0 {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "status is required", "invalid_body")
		return
	}
	if err := h.Validator.ValidateRegistration(h.Op, p.Status, p.Body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"registered response violates schema for status "+strconv.Itoa(p.Status)+": "+err.Error(),
			"invalid_match")
		return
	}

	// Strip /match from the URL path to derive the registration key.
	regPath := strings.TrimSuffix(r.URL.Path, "/match")
	h.Store.Put(matches.Match{
		Method:  h.Op.Method,
		Path:    regPath,
		Kind:    KindOfPath(regPath),
		Status:  p.Status,
		Headers: p.Headers,
		Body:    p.Body,
	})
	w.WriteHeader(http.StatusNoContent)
}
