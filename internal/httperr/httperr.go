// Package httperr writes JSON error responses in either Auth0 Management
// API shape or Auth0 Authentication API shape.
package httperr

import (
	"encoding/json"
	"net/http"
)

// MgmtError matches Auth0 Management API error responses.
type MgmtError struct {
	StatusCode int    `json:"statusCode"`
	Error      string `json:"error"`
	Message    string `json:"message"`
	ErrorCode  string `json:"errorCode,omitempty"`
}

// AuthError matches Auth0 Authentication API error responses.
type AuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// WriteMgmt writes a Management-API-shaped JSON error.
//
// Encode errors here only fire when the client has already disconnected
// (broken pipe, write deadline) — the status line and headers have already
// been written by that point and there is nothing useful to do about it, so
// the result is intentionally discarded.
func WriteMgmt(w http.ResponseWriter, status int, errStr, message, errorCode string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(MgmtError{
		StatusCode: status,
		Error:      errStr,
		Message:    message,
		ErrorCode:  errorCode,
	})
}

// WriteAuth writes an Authentication-API-shaped JSON error. Same disconnect
// semantics as WriteMgmt — see its doc for why the encode error is dropped.
func WriteAuth(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(AuthError{
		Error:            errCode,
		ErrorDescription: description,
	})
}
