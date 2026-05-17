package admin0

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/clock"
	"github.com/sergiught/auth0-mock/internal/httperr"
)

// decodeClockBody decodes body strictly (rejects unknown fields) and
// classifies the failure mode into one of:
//
//   - "invalid_clock_field" — body contained a JSON field not in the
//     target struct (typos like {"noow":"..."}). Distinct so callers
//     can spot misspellings without parsing the human-readable message.
//   - "invalid_clock_request" — everything else (malformed JSON, wrong
//     types, etc.).
//
// encoding/json doesn't expose a typed unknown-field error, so the
// canonical detection is a prefix check on its hardcoded format string
// ("json: unknown field %q"). The fmt.Errorf call site is at
// src/encoding/json/decode.go:740 in Go 1.26 and has been stable
// across major versions; if it ever moves, the unknown-field path
// degrades to invalid_clock_request, not a hard failure.
func decodeClockBody(r *http.Request, target any) (errorCode string, err error) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			return "invalid_clock_field", err
		}
		return "invalid_clock_request", err
	}
	return "", nil
}

// clockGetResponse is the wire shape returned by GET /admin0/clock.
// `now` is RFC 3339 UTC with second precision. `offset` is only
// populated when the clock is in offset mode — callers can read back
// the configured skew without computing it from now - wall_clock.
type clockGetResponse struct {
	Mode   string `json:"mode"`
	Now    string `json:"now"`
	Offset string `json:"offset,omitempty"`
}

// clockPutBody mirrors PUT /admin0/clock. Exactly one of Now/Offset
// must be set — both, or neither, returns 400 invalid_clock_request.
type clockPutBody struct {
	Now    *string `json:"now,omitempty"`
	Offset *string `json:"offset,omitempty"`
}

// clockAdvanceBody mirrors POST /admin0/clock/advance.
type clockAdvanceBody struct {
	By string `json:"by"`
}

// GetClockHandler reports the current clock mode and resolved Now.
type GetClockHandler struct {
	Clock *clock.Controlled
}

func (h *GetClockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mode, now := h.Clock.State()
	resp := clockGetResponse{
		Mode: string(mode),
		Now:  now.UTC().Format(time.RFC3339),
	}
	if off, ok := h.Clock.ConfiguredOffset(); ok {
		resp.Offset = off.String()
	}
	render.JSON(w, r, resp)
}

// PutClockHandler freezes the clock to a specific instant or switches
// it into offset mode. Body must contain exactly one of `now` / `offset`.
type PutClockHandler struct {
	Clock *clock.Controlled
}

func (h *PutClockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body clockPutBody
	if code, err := decodeClockBody(r, &body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"decode body: "+err.Error(), code)
		return
	}
	hasNow := body.Now != nil
	hasOff := body.Offset != nil
	if hasNow == hasOff { // both true or both false
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			`specify exactly one of "now" or "offset"`,
			"invalid_clock_request")
		return
	}
	if hasNow {
		t, err := time.Parse(time.RFC3339, *body.Now)
		if err != nil {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				`"now" is not RFC 3339: `+err.Error(),
				"invalid_clock_time")
			return
		}
		h.Clock.Freeze(t)
	} else {
		d, err := time.ParseDuration(*body.Offset)
		if err != nil {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				`"offset" is not a Go duration: `+err.Error(),
				"invalid_clock_duration")
			return
		}
		h.Clock.Offset(d)
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdvanceClockHandler mutates the held value by `by`. Returns 400
// invalid_clock_state when the clock is in real mode.
type AdvanceClockHandler struct {
	Clock *clock.Controlled
}

func (h *AdvanceClockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body clockAdvanceBody
	if code, err := decodeClockBody(r, &body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"decode body: "+err.Error(), code)
		return
	}
	d, err := time.ParseDuration(body.By)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			`"by" is not a Go duration: `+err.Error(),
			"invalid_clock_duration")
		return
	}
	if err := h.Clock.Advance(d); err != nil {
		if errors.Is(err, clock.ErrAdvanceInRealMode) {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				err.Error(), "invalid_clock_state")
			return
		}
		httperr.WriteMgmt(w, http.StatusInternalServerError,
			"Internal Server Error", err.Error(), "clock_advance_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteClockHandler restores the clock to real mode.
type DeleteClockHandler struct {
	Clock *clock.Controlled
}

func (h *DeleteClockHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.Clock.Reset()
	w.WriteHeader(http.StatusNoContent)
}
