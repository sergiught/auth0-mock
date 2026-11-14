package authapi

import (
	"maps"
	"net/http"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/session"
)

// mfaRejection is the 4xx a request with a wrong or missing MFA code gets
// once its mfa_token has been found valid.
type mfaRejection struct {
	status int
	code   string
	desc   string
}

// respondMFAOTP handles grant_type=http://auth0.com/oauth/grant-type/mfa-otp.
// Trades a previously-issued mfa_token + a TOTP/HOTP code for an access_token.
func (h *TokenHandler) respondMFAOTP(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	var reject *mfaRejection
	if req.OTP != mfa.AcceptedOTP {
		reject = &mfaRejection{http.StatusForbidden, "invalid_grant", "Wrong otp"}
	}
	ctx, ok := h.redeemMFAToken(w, req.MFAToken, reject)
	if !ok {
		return
	}
	h.mintFromMFA(w, r, ctx, "mfa-otp", req.Params)
}

// respondMFAOOB handles grant_type=http://auth0.com/oauth/grant-type/mfa-oob
// (out-of-band challenge — push notification or SMS confirmation).
func (h *TokenHandler) respondMFAOOB(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	var reject *mfaRejection
	switch {
	case req.OOBCode == "":
		reject = &mfaRejection{http.StatusBadRequest, "invalid_request", "missing oob_code"}
	// Binding_code is required when the user is asked to type a number they
	// see on a second device (e.g. SMS code, push-with-numbers).
	case req.BindingCode != mfa.AcceptedBindingCode:
		reject = &mfaRejection{http.StatusForbidden, "invalid_grant", "Wrong binding code"}
	}
	ctx, ok := h.redeemMFAToken(w, req.MFAToken, reject)
	if !ok {
		return
	}
	h.mintFromMFA(w, r, ctx, "mfa-oob", req.Params)
}

// respondMFARecoveryCode handles
// grant_type=http://auth0.com/oauth/grant-type/mfa-recovery-code.
func (h *TokenHandler) respondMFARecoveryCode(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	var reject *mfaRejection
	if req.RecoveryCode != mfa.AcceptedRecoveryCode {
		reject = &mfaRejection{http.StatusForbidden, "invalid_grant", "Wrong recovery code"}
	}
	ctx, ok := h.redeemMFAToken(w, req.MFAToken, reject)
	if !ok {
		return
	}
	h.mintFromMFA(w, r, ctx, "mfa-recovery-code", req.Params)
}

// redeemMFAToken validates an mfa_token and, when reject is nil (the code was
// accepted), consumes it. A rejected code only peeks at the token, so it can
// be retried, and gets reject as its response once the token is known valid.
// Consume is atomic, so of several concurrent correct submissions only one
// wins; the others get the same error as a reused token. On failure, writes
// the 4xx response and returns ok=false.
func (h *TokenHandler) redeemMFAToken(w http.ResponseWriter, token string, reject *mfaRejection) (mfa.Context, bool) {
	if h.MFA == nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "mfa not configured on this server")
		return mfa.Context{}, false
	}
	if token == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing mfa_token")
		return mfa.Context{}, false
	}
	lookup := h.MFA.Consume
	if reject != nil {
		lookup = h.MFA.Peek
	}
	ctx, ok := lookup(token)
	if !ok {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Malformed or expired mfa_token")
		return mfa.Context{}, false
	}
	if reject != nil {
		httperr.WriteAuth(w, reject.status, reject.code, reject.desc)
		return mfa.Context{}, false
	}
	return ctx, true
}

// mintFromMFA issues the access_token + id_token + refresh_token after a
// successful MFA step-up. The gty claim reflects the specific MFA grant
// (mfa-otp / mfa-oob / mfa-recovery-code) so downstream services can tell
// MFA-stepped-up tokens apart from regular ones.
func (h *TokenHandler) mintFromMFA(w http.ResponseWriter, r *http.Request, ctx mfa.Context, gty string, params map[string]any) {
	extra := map[string]any{"gty": gty, "azp": ctx.ClientID}
	if ctx.Realm != "" {
		extra["connection"] = ctx.Realm
	}
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  ctx.Subject,
		Audience: []string{ctx.Audience},
		Scope:    ctx.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(extra, ctx.Audience, params),
	}, ctx.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	authTime := h.Keys.Cfg().Now()
	var idExtra map[string]any
	if ctx.Profile != nil {
		idExtra = maps.Clone(ctx.Profile)
		idExtra["azp"] = ctx.ClientID
		idExtra["auth_time"] = authTime.Unix()
	} else {
		idExtra = fillProfile(map[string]any{
			"email":          ctx.Subject,
			"email_verified": true,
			"name":           ctx.Subject,
			"azp":            ctx.ClientID,
			"auth_time":      authTime.Unix(),
		}, ctx.Subject)
	}
	if ctx.Realm != "" {
		idExtra["connection"] = ctx.Realm
	}
	id, err := h.Keys.Mint(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  ctx.Subject,
		Audience: []string{ctx.ClientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra:    idExtra,
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	h.rememberProfile(ctx.Subject, idExtra)
	render.JSON(w, r, tokenResponse{
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: h.offlineRefreshToken(session.Grant{Subject: ctx.Subject, ClientID: ctx.ClientID, Audience: ctx.Audience, Scope: ctx.Scope, Realm: ctx.Realm, AuthTime: authTime}, idExtra),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        ctx.Scope,
	})
}
