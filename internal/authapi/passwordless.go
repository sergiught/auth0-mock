package authapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/httperr"
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

// passwordlessStartResponse mirrors Auth0's POST /passwordless/start
// response: the email / phone_number fields echo back the address the
// flow was started for, NOT the connection name. See:
// https://auth0.com/docs/api/authentication#get-code-or-link
type passwordlessStartResponse struct {
	ID    string `json:"_id"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone_number,omitempty"`
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
		ID:    uuid.NewString(),
		Email: req.Email,
		Phone: req.PhoneNumber,
	})
}

// PasswordlessVerifyHandler verifies a passwordless OTP and mints tokens. It
// is a thin wrapper over the passwordless grant on /oauth/token.
type PasswordlessVerifyHandler struct {
	Token *TokenHandler
}

func (h *PasswordlessVerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenRequest(r)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.GrantType) != grantPasswordlessOTP {
		httperr.WriteAuth(w, http.StatusBadRequest, "unsupported_grant_type", req.GrantType)
		return
	}
	aud := req.Audience
	if aud == "" {
		aud = h.Token.DefaultAudience
	}
	h.Token.respondPasswordless(w, r, req, aud)
}
