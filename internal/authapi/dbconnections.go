package authapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

type signupRequest struct {
	ClientID   string `json:"client_id"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Connection string `json:"connection"`
	Username   string `json:"username,omitempty"`
}

type signupResponse struct {
	ID            string `json:"_id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

type changePasswordRequest struct {
	ClientID   string `json:"client_id"`
	Email      string `json:"email"`
	Connection string `json:"connection"`
}

// DBConnectionsSignupHandler handles user signup via database connections.
type DBConnectionsSignupHandler struct{}

func (h *DBConnectionsSignupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Email == "" || req.Connection == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "email and connection are required")
		return
	}
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, signupResponse{
		ID:            uuid.NewString(),
		Email:         req.Email,
		EmailVerified: false,
	})
}

// DBConnectionsChangePasswordHandler handles password change requests.
type DBConnectionsChangePasswordHandler struct{}

func (h *DBConnectionsChangePasswordHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Email == "" || req.Connection == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "email and connection are required")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("We've just sent you an email to reset your password."))
}
