package auth0mock

import (
	"context"
	"net/http"
	"net/url"
)

// AuthorizeError is a rule that makes /authorize redirect back to the
// caller's redirect_uri with an OAuth error instead of logging in.
// Match is an optional set of /authorize query parameters (for example
// client_id or prompt) that must all equal the request's values; an
// empty Match applies to every request. ID is assigned by the mock and
// is left empty when adding a rule.
type AuthorizeError struct {
	ID               string            `json:"id,omitempty"`
	Match            map[string]string `json:"match,omitempty"`
	Error            string            `json:"error"`
	ErrorDescription string            `json:"error_description,omitempty"`
}

// AuthorizeErrorsClient owns /admin0/authorize-errors — rules that make
// /authorize answer with an OAuth error. Reach it via
// Client.AuthorizeErrors.
//
// Rules are global to the mock process and are cleared by Client.Reset.
type AuthorizeErrorsClient struct {
	c *Client
}

// Add registers a rule and returns it as stored, with its ID set.
func (a *AuthorizeErrorsClient) Add(ctx context.Context, e AuthorizeError) (AuthorizeError, error) {
	var out AuthorizeError
	if err := a.c.do(ctx, http.MethodPost, "/admin0/authorize-errors", e, &out); err != nil {
		return AuthorizeError{}, err
	}
	return out, nil
}

// List returns every rule in registration order.
func (a *AuthorizeErrorsClient) List(ctx context.Context) ([]AuthorizeError, error) {
	var resp struct {
		Rules []AuthorizeError `json:"rules"`
	}
	if err := a.c.do(ctx, http.MethodGet, "/admin0/authorize-errors", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

// Delete removes one rule by ID. An unknown ID is a no-op.
func (a *AuthorizeErrorsClient) Delete(ctx context.Context, id string) error {
	return a.c.do(ctx, http.MethodDelete, "/admin0/authorize-errors/"+url.PathEscape(id), nil, nil)
}

// Clear removes every rule.
func (a *AuthorizeErrorsClient) Clear(ctx context.Context) error {
	return a.c.do(ctx, http.MethodDelete, "/admin0/authorize-errors", nil, nil)
}
