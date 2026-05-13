package authapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
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

func passwordlessStart(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(passwordlessStartResponse{
			ID:         uuid.NewString(),
			Connection: req.Connection,
			Phone:      req.PhoneNumber,
		})
	}
}

func passwordlessVerify(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		access, err := d.Keys.Mint(jwks.MintOpts{
			Subject:  username,
			Audience: []string{d.DefaultAudience},
			TTL:      d.Keys.Cfg().AccessTokenTTL,
			Extra:    map[string]any{"gty": "passwordless", "azp": clientID},
		})
		if err != nil {
			httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: access,
			TokenType:   "Bearer",
			ExpiresIn:   int64(d.Keys.Cfg().AccessTokenTTL.Seconds()),
		})
	}
}
