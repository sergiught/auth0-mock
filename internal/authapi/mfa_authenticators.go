package authapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/bearer"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/pathparam"
)

// MFAAuthenticatorsHandler serves GET /mfa/authenticators and
// DELETE /mfa/authenticators/{id}.
type MFAAuthenticatorsHandler struct {
	MFA  *mfa.Store
	Keys *jwks.KeySet
}

type authenticatorBody struct {
	ID                string `json:"id"`
	AuthenticatorType string `json:"authenticator_type"`
	Active            bool   `json:"active"`
	OOBChannel        string `json:"oob_channel,omitempty"`
	Name              string `json:"name,omitempty"`
}

// authFailure is an error response to write as is.
type authFailure struct {
	status     int
	code, desc string
}

// resolveUser authenticates the bearer and finds the user's key and config.
// With allowMFAToken an mfa_token is peeked, so it stays redeemable. Any
// other bearer must be an access token for the mfa audience carrying scope.
func (h *MFAAuthenticatorsHandler) resolveUser(r *http.Request, scope string, allowMFAToken bool) (
	key string, cfg mfa.UserConfig, found bool, fail *authFailure,
) {
	token, ok := bearer.Token(r.Header.Get("Authorization"))
	if !ok {
		return "", cfg, false, &authFailure{http.StatusUnauthorized, "invalid_token", "missing bearer token"}
	}
	if allowMFAToken {
		if ctx, peeked := h.MFA.Peek(token); peeked {
			key, cfg, found = h.MFA.Lookup(ctx)
			return key, cfg, found, nil
		}
	}
	issuer := h.Keys.IssuerFor(r)
	claims, err := h.Keys.Verify(token, jwks.VerifyOpts{
		Issuer: issuer, RequireAudience: strings.TrimSuffix(issuer, "/") + "/mfa/",
	})
	if err != nil {
		return "", cfg, false, &authFailure{http.StatusUnauthorized, "invalid_token", err.Error()}
	}
	if !slices.Contains(strings.Fields(claims.Scope), scope) {
		return "", cfg, false, &authFailure{http.StatusForbidden, "insufficient_scope", "Insufficient scope, expected: " + scope}
	}
	key, cfg, found = h.MFA.Lookup(mfa.Context{Subject: claims.Subject})
	return key, cfg, found, nil
}

func (h *MFAAuthenticatorsHandler) ready(w http.ResponseWriter) bool {
	if h.MFA == nil || h.Keys == nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "mfa not configured on this server")
		return false
	}
	return true
}

// List serves GET /mfa/authenticators.
func (h *MFAAuthenticatorsHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	_, cfg, _, fail := h.resolveUser(r, "read:authenticators", true)
	if fail != nil {
		httperr.WriteAuth(w, fail.status, fail.code, fail.desc)
		return
	}
	out := make([]authenticatorBody, 0, len(cfg.Authenticators))
	for _, a := range cfg.Authenticators {
		b := authenticatorBody{ID: a.ID, AuthenticatorType: a.Type, Active: a.Active != nil && *a.Active, Name: a.Name}
		if a.Type == mfa.TypeOOB {
			b.OOBChannel = a.OOBChannel
		}
		out = append(out, b)
	}
	render.JSON(w, r, out)
}

// Delete serves DELETE /mfa/authenticators/{id}.
func (h *MFAAuthenticatorsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	key, _, found, fail := h.resolveUser(r, "remove:authenticators", false)
	if fail != nil {
		httperr.WriteAuth(w, fail.status, fail.code, fail.desc)
		return
	}
	id, err := pathparam.Get(r, "id")
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "invalid id: "+err.Error())
		return
	}
	if !found || !h.MFA.RemoveAuthenticator(key, id) {
		httperr.WriteAuth(w, http.StatusNotFound, "not_found", "Authenticator not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
