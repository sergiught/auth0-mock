package authapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/permissions"
)

// The mock accepts only this fixed OTP. Tests can rely on it being constant.
const acceptedPasswordlessOTP = "000000"

type passwordlessStartRequest struct {
	ClientID    string         `json:"client_id"`
	Connection  string         `json:"connection"`
	Email       string         `json:"email,omitempty"`
	PhoneNumber string         `json:"phone_number,omitempty"`
	Send        string         `json:"send,omitempty"`
	AuthParams  map[string]any `json:"authParams,omitempty"`
}

type passwordlessStartResponse struct {
	ID         string `json:"_id"`
	Connection string `json:"email,omitempty"` // Auth0's response uses key "email" for the connection name on email flows
	Phone      string `json:"phone_number,omitempty"`
}

// PasswordlessStartHandler initiates a passwordless authentication flow.
type PasswordlessStartHandler struct{}

func (h *PasswordlessStartHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req passwordlessStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Connection == "" || (req.Email == "" && req.PhoneNumber == "") {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request",
			"connection and one of (email, phone_number) are required")
		return
	}
	render.JSON(w, r, passwordlessStartResponse{
		ID:         uuid.NewString(),
		Connection: req.Connection,
		Phone:      req.PhoneNumber,
	})
}

// PasswordlessVerifyHandler verifies a passwordless OTP and mints a token.
type PasswordlessVerifyHandler struct {
	Keys            *jwks.KeySet
	DefaultAudience string
	Claims          *claims.Store
	Permissions     *permissions.Store
}

func (h *PasswordlessVerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	gt := strings.TrimSpace(r.PostForm.Get("grant_type"))
	if gt != "http://auth0.com/oauth/grant-type/passwordless/otp" {
		httperr.WriteAuth(w, http.StatusBadRequest, "unsupported_grant_type", gt)
		return
	}
	username := r.PostForm.Get("username")
	clientID := r.PostForm.Get("client_id")
	otp := r.PostForm.Get("otp")
	if otp != acceptedPasswordlessOTP {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Wrong email or verification code")
		return
	}

	extra := map[string]any{"gty": "passwordless", "azp": clientID}
	if h.Permissions != nil {
		if perms := h.Permissions.Get(h.DefaultAudience); len(perms) > 0 {
			extra["permissions"] = perms
		}
	}
	if h.Claims != nil {
		h.Claims.MergeInto(extra)
	}
	access, err := h.Keys.Mint(jwks.MintOpts{
		Subject:  username,
		Audience: []string{h.DefaultAudience},
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    extra,
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
