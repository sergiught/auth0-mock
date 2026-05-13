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

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
)

// TokenHandler handles OAuth token requests.
type TokenHandler struct {
	Keys            *jwks.KeySet
	Issuer          string
	DefaultAudience string
	Log             zerolog.Logger
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
	case "refresh_token":
		h.respondRefreshToken(w, r, req, aud)
	case "authorization_code":
		h.respondAuthorizationCode(w, r, req, aud)
	default:
		httperr.WriteAuth(w, http.StatusBadRequest, "unsupported_grant_type",
			"grant_type "+req.GrantType+" is not supported")
	}
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
	}, nil
}

func (h *TokenHandler) respondClientCredentials(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  req.ClientID + "@clients",
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    map[string]any{"gty": "client-credentials", "azp": req.ClientID},
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
		Extra:    map[string]any{"gty": "password", "azp": req.ClientID},
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
		Extra:    map[string]any{"gty": "refresh-token", "azp": req.ClientID},
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
	subject := "auth0|" + uuid.NewString()
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    map[string]any{"gty": "authorization-code", "azp": req.ClientID},
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
