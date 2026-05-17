// Package admin0 exposes the mock's control-plane endpoints under /admin0/*.
//
// These endpoints are NEVER authenticated — they're meant for test setup and
// teardown from outside the bearer-protected Mgmt API surface.
package admin0

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Deps groups the in-memory stores admin0 controls.
type Deps struct {
	Matches     *matches.Store
	Claims      *claims.Store
	Permissions *permissions.Store
	MFA         *mfa.Store
	Validator   *spec.Validator
	Clock       *clock.Controlled
}

// Mount registers every /admin0/* route on r.
func Mount(r chi.Router, d Deps) {
	r.Method(http.MethodPost, "/admin0/reset", &ResetHandler{Deps: d})
	r.Method(http.MethodPost, "/admin0/expectations", &PostExpectationHandler{Store: d.Matches, Validator: d.Validator})
	r.Method(http.MethodGet, "/admin0/expectations", &ListExpectationsHandler{Store: d.Matches})
	r.Method(http.MethodDelete, "/admin0/expectations", &DeleteExpectationsHandler{Store: d.Matches})
	// Per-ID GET / DELETE for the SDK's RegisteredExpectation handle
	// (Hits() / Clear() respectively).
	r.Method(http.MethodGet, "/admin0/expectations/{id}", &GetExpectationByIDHandler{Store: d.Matches})
	r.Method(http.MethodDelete, "/admin0/expectations/{id}", &DeleteExpectationByIDHandler{Store: d.Matches})

	r.Method(http.MethodGet, "/admin0/claims", &GetClaimsHandler{Store: d.Claims})
	r.Method(http.MethodPut, "/admin0/claims", &PutClaimsHandler{Store: d.Claims})
	r.Method(http.MethodDelete, "/admin0/claims", &DeleteClaimsHandler{Store: d.Claims})

	r.Method(http.MethodGet, "/admin0/permissions", &GetAllPermissionsHandler{Store: d.Permissions})
	r.Method(http.MethodDelete, "/admin0/permissions", &DeleteAllPermissionsHandler{Store: d.Permissions})
	// Audiences are often URLs (e.g. "https://api.example.com/") that contain
	// slashes. Chi's single-segment "{audience}" param won't match those, so
	// we use a catch-all wildcard.
	r.Method(http.MethodGet, "/admin0/permissions/*", &GetPermissionsHandler{Store: d.Permissions})
	r.Method(http.MethodPut, "/admin0/permissions/*", &PutPermissionsHandler{Store: d.Permissions})
	r.Method(http.MethodDelete, "/admin0/permissions/*", &DeletePermissionsHandler{Store: d.Permissions})

	r.Method(http.MethodGet, "/admin0/mfa-required", &GetMFARequiredHandler{Store: d.MFA})
	r.Method(http.MethodPut, "/admin0/mfa-required", &PutMFARequiredHandler{Store: d.MFA})

	r.Method(http.MethodGet, "/admin0/clock", &GetClockHandler{Clock: d.Clock})
	r.Method(http.MethodPut, "/admin0/clock", &PutClockHandler{Clock: d.Clock})
	r.Method(http.MethodPost, "/admin0/clock/advance", &AdvanceClockHandler{Clock: d.Clock})
	r.Method(http.MethodDelete, "/admin0/clock", &DeleteClockHandler{Clock: d.Clock})
}

// ResetHandler wipes every store admin0 governs: registered matches, custom
// claims, and per-audience permissions.
type ResetHandler struct {
	Deps Deps
}

func (h *ResetHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if h.Deps.Matches != nil {
		h.Deps.Matches.ResetAll()
	}
	if h.Deps.Claims != nil {
		h.Deps.Claims.Clear()
	}
	if h.Deps.Permissions != nil {
		h.Deps.Permissions.Clear()
	}
	if h.Deps.MFA != nil {
		h.Deps.MFA.Reset()
	}
	if h.Deps.Clock != nil {
		h.Deps.Clock.Reset()
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- mfa ------------------------------------------------------------------.

type mfaRequiredBody struct {
	Required bool `json:"required"`
}

// GetMFARequiredHandler reports whether the password and password-realm grants
// currently demand MFA step-up.
type GetMFARequiredHandler struct {
	Store *mfa.Store
}

func (h *GetMFARequiredHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, mfaRequiredBody{Required: h.Store.IsRequired()})
}

// PutMFARequiredHandler toggles MFA enforcement at runtime. Body: {"required":true|false}.
type PutMFARequiredHandler struct {
	Store *mfa.Store
}

func (h *PutMFARequiredHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body mfaRequiredBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	h.Store.SetRequired(body.Required)
	w.WriteHeader(http.StatusNoContent)
}

// --- claims ----------------------------------------------------------------.

// GetClaimsHandler returns the per-process custom-claim map.
type GetClaimsHandler struct {
	Store *claims.Store
}

func (h *GetClaimsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, h.Store.Get())
}

// PutClaimsHandler replaces the per-process custom-claim map with the JSON
// object in the request body.
type PutClaimsHandler struct {
	Store *claims.Store
}

func (h *PutClaimsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	h.Store.Set(body)
	w.WriteHeader(http.StatusNoContent)
}

// DeleteClaimsHandler clears every custom claim.
type DeleteClaimsHandler struct {
	Store *claims.Store
}

func (h *DeleteClaimsHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.Store.Clear()
	w.WriteHeader(http.StatusNoContent)
}

// --- permissions -----------------------------------------------------------.

// GetAllPermissionsHandler returns the full per-audience permission map.
type GetAllPermissionsHandler struct {
	Store *permissions.Store
}

func (h *GetAllPermissionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, h.Store.All())
}

// DeleteAllPermissionsHandler removes every audience's permissions.
type DeleteAllPermissionsHandler struct {
	Store *permissions.Store
}

func (h *DeleteAllPermissionsHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.Store.Clear()
	w.WriteHeader(http.StatusNoContent)
}

// GetPermissionsHandler returns the permissions registered for one audience.
type GetPermissionsHandler struct {
	Store *permissions.Store
}

func (h *GetPermissionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	audience := chi.URLParam(r, "*")
	perms := h.Store.Get(audience)
	if perms == nil {
		perms = []string{}
	}
	render.JSON(w, r, perms)
}

// PutPermissionsHandler sets the permissions for one audience to the JSON
// array in the request body.
type PutPermissionsHandler struct {
	Store *permissions.Store
}

func (h *PutPermissionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	audience := chi.URLParam(r, "*")
	var perms []string
	if err := json.NewDecoder(r.Body).Decode(&perms); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	h.Store.Set(audience, perms)
	w.WriteHeader(http.StatusNoContent)
}

// DeletePermissionsHandler clears the permissions for one audience.
type DeletePermissionsHandler struct {
	Store *permissions.Store
}

func (h *DeletePermissionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	audience := chi.URLParam(r, "*")
	h.Store.Delete(audience)
	w.WriteHeader(http.StatusNoContent)
}
