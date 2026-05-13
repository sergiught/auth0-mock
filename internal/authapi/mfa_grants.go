package authapi

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
)

// respondMFAOTP handles grant_type=http://auth0.com/oauth/grant-type/mfa-otp.
// Trades a previously-issued mfa_token + a TOTP/HOTP code for an access_token.
func (h *TokenHandler) respondMFAOTP(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	ctx, ok := h.consumeMFAToken(w, req.MFAToken)
	if !ok {
		return
	}
	if req.OTP != mfa.AcceptedOTP {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Wrong otp")
		return
	}
	h.mintFromMFA(w, r, ctx, "mfa-otp")
}

// respondMFAOOB handles grant_type=http://auth0.com/oauth/grant-type/mfa-oob
// (out-of-band challenge — push notification or SMS confirmation).
func (h *TokenHandler) respondMFAOOB(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	ctx, ok := h.consumeMFAToken(w, req.MFAToken)
	if !ok {
		return
	}
	if req.OOBCode == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing oob_code")
		return
	}
	// binding_code is required when the user is asked to type a number they
	// see on a second device (e.g. SMS code, push-with-numbers).
	if req.BindingCode != mfa.AcceptedBindingCode {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Wrong binding code")
		return
	}
	h.mintFromMFA(w, r, ctx, "mfa-oob")
}

// respondMFARecoveryCode handles
// grant_type=http://auth0.com/oauth/grant-type/mfa-recovery-code.
func (h *TokenHandler) respondMFARecoveryCode(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	ctx, ok := h.consumeMFAToken(w, req.MFAToken)
	if !ok {
		return
	}
	if req.RecoveryCode != mfa.AcceptedRecoveryCode {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Wrong recovery code")
		return
	}
	h.mintFromMFA(w, r, ctx, "mfa-recovery-code")
}

// consumeMFAToken validates and consumes an mfa_token. On failure, writes the
// appropriate 4xx response and returns ok=false.
func (h *TokenHandler) consumeMFAToken(w http.ResponseWriter, token string) (mfa.Context, bool) {
	if h.MFA == nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "mfa not configured on this server")
		return mfa.Context{}, false
	}
	if token == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing mfa_token")
		return mfa.Context{}, false
	}
	ctx, ok := h.MFA.Consume(token)
	if !ok {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Malformed or expired mfa_token")
		return mfa.Context{}, false
	}
	return ctx, true
}

// mintFromMFA issues the access_token + id_token + refresh_token after a
// successful MFA step-up. The gty claim reflects the specific MFA grant
// (mfa-otp / mfa-oob / mfa-recovery-code) so downstream services can tell
// MFA-stepped-up tokens apart from regular ones.
func (h *TokenHandler) mintFromMFA(w http.ResponseWriter, r *http.Request, ctx mfa.Context, gty string) {
	extra := map[string]any{"gty": gty, "azp": ctx.ClientID}
	if ctx.Realm != "" {
		extra["connection"] = ctx.Realm
	}
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  ctx.Subject,
		Audience: []string{ctx.Audience},
		Scope:    ctx.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(extra, ctx.Audience),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	id, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  ctx.Subject,
		Audience: []string{ctx.ClientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra: map[string]any{
			"email":          ctx.Subject,
			"email_verified": true,
			"name":           ctx.Subject,
		},
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	render.JSON(w, r, tokenResponse{
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: uuid.NewString(),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        ctx.Scope,
	})
}
