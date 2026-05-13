package authapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
)

// TokenHandler handles OAuth token requests.
type TokenHandler struct {
	Keys            *jwks.KeySet
	Issuer          string
	DefaultAudience string
	Log             zerolog.Logger
	Claims          *claims.Store
	Permissions     *permissions.Store
	// PKCE may be nil. When set and the authorization_code grant supplies a
	// code that was stashed at /authorize with a code_challenge, the matching
	// code_verifier is required and verified.
	PKCE *pkce.Store
}

// augmentExtra layers per-audience permissions and per-process custom claims
// onto the Extra map passed to jwks.Mint. Custom claims take final precedence,
// allowing tests to override anything (gty, azp, even permissions).
func (h *TokenHandler) augmentExtra(extra map[string]any, audience string) map[string]any {
	if extra == nil {
		extra = make(map[string]any)
	}
	if h.Permissions != nil {
		if perms := h.Permissions.Get(audience); len(perms) > 0 {
			extra["permissions"] = perms
		}
	}
	if h.Claims != nil {
		h.Claims.MergeInto(extra)
	}
	return extra
}

func (h *TokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenRequest(r)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.GrantType == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing grant_type")
		return
	}

	aud := req.Audience
	if aud == "" {
		aud = h.DefaultAudience
	}

	switch req.GrantType {
	case "client_credentials":
		h.respondClientCredentials(w, r, req, aud)
	case "password":
		h.respondPassword(w, r, req, aud)
	case "http://auth0.com/oauth/grant-type/password-realm":
		h.respondPasswordRealm(w, r, req, aud)
	case "refresh_token":
		h.respondRefreshToken(w, r, req, aud)
	case "authorization_code":
		h.respondAuthorizationCode(w, r, req, aud)
	default:
		httperr.WriteAuth(w, http.StatusBadRequest, "unsupported_grant_type",
			"grant_type "+req.GrantType+" is not supported")
	}
}

// respondPasswordRealm handles Auth0's password-realm grant, which is the
// password grant plus a `realm` parameter selecting which connection to
// authenticate against (e.g. "Username-Password-Authentication" vs an
// enterprise connection). Used by Auth0 Native SDKs.
func (h *TokenHandler) respondPasswordRealm(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	if req.Realm == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing realm")
		return
	}
	subject := req.Username
	if subject == "" {
		subject = "auth0|" + uuid.NewString()
	}
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra: h.augmentExtra(map[string]any{
			"gty":              "password-realm",
			"azp":              req.ClientID,
			"connection":       req.Realm,
			"https://auth0.com/realm": req.Realm,
		}, aud),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	id, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{req.ClientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra: map[string]any{
			"email":          subject,
			"email_verified": true,
			"name":           subject,
			"connection":     req.Realm,
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
		Scope:        req.Scope,
	})
}

// parseTokenRequest accepts either application/json or
// application/x-www-form-urlencoded.
func parseTokenRequest(r *http.Request) (*tokenRequest, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		var req tokenRequest
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
		}
		return &req, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	if r.PostForm == nil {
		return nil, errors.New("no form body")
	}
	return &tokenRequest{
		GrantType:    r.PostForm.Get("grant_type"),
		ClientID:     r.PostForm.Get("client_id"),
		ClientSecret: r.PostForm.Get("client_secret"),
		Audience:     r.PostForm.Get("audience"),
		Scope:        r.PostForm.Get("scope"),
		Username:     r.PostForm.Get("username"),
		Password:     r.PostForm.Get("password"),
		RefreshToken: r.PostForm.Get("refresh_token"),
		Code:         r.PostForm.Get("code"),
		RedirectURI:  r.PostForm.Get("redirect_uri"),
		CodeVerifier: r.PostForm.Get("code_verifier"),
		Realm:        r.PostForm.Get("realm"),
	}, nil
}

func (h *TokenHandler) respondClientCredentials(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  req.ClientID + "@clients",
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "client-credentials", "azp": req.ClientID}, aud),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	render.JSON(w, r, tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:       req.Scope,
	})
}

func (h *TokenHandler) respondPassword(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	subject := req.Username
	if subject == "" {
		subject = "auth0|" + uuid.NewString()
	}
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "password", "azp": req.ClientID}, aud),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	id, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{req.ClientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra: map[string]any{
			"email":          subject,
			"email_verified": true,
			"name":           subject,
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
		Scope:        req.Scope,
	})
}

func (h *TokenHandler) respondRefreshToken(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  req.ClientID + "@refresh",
		Audience: []string{aud},
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "refresh-token", "azp": req.ClientID}, aud),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	render.JSON(w, r, tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
	})
}

func (h *TokenHandler) respondAuthorizationCode(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	// Verify PKCE if the /authorize step stashed a challenge for this code.
	if h.PKCE != nil && req.Code != "" {
		if entry, ok := h.PKCE.Consume(req.Code); ok {
			if err := entry.Verify(req.CodeVerifier); err != nil {
				httperr.WriteAuth(w, http.StatusBadRequest, "invalid_grant",
					"PKCE verification failed: "+err.Error())
				return
			}
		}
	}
	subject := "auth0|" + uuid.NewString()
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "authorization-code", "azp": req.ClientID}, aud),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	id, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{req.ClientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra:    map[string]any{"email": subject + "@example.com"},
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
		Scope:        req.Scope,
	})
}
