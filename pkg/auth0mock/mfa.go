package auth0mock

import (
	"context"
	"net/http"
	"net/url"
)

// MFAClient owns the MFA admin routes. Get and Set drive
// /admin0/mfa-required, a global boolean that controls whether the
// password and password-realm grants demand an MFA step-up before
// issuing a token. SetUser, GetUser, DeleteUser and ListUsers manage
// per-user MFA configuration under /admin0/mfa/users. Reach it via
// Client.MFA.
type MFAClient struct {
	c *Client
}

// mfaRequiredBody is the wire shape both directions ({"required":bool}).
// Unexported so callers don't accidentally inline-construct it.
type mfaRequiredBody struct {
	Required bool `json:"required"`
}

// Get reports whether the password and password-realm grants
// currently require an MFA step-up. Mirrors the Get/Set verb shape
// of ClaimsClient and PermissionsClient — the `Required` suffix the
// previous IsRequired/SetRequired pair carried is redundant on this
// receiver.
func (m *MFAClient) Get(ctx context.Context) (bool, error) {
	var resp mfaRequiredBody
	if err := m.c.do(ctx, http.MethodGet, "/admin0/mfa-required", nil, &resp); err != nil {
		return false, err
	}
	return resp.Required, nil
}

// Set toggles MFA enforcement. Idempotent — calling with the same
// value twice in a row is a no-op from the test's point of view.
func (m *MFAClient) Set(ctx context.Context, required bool) error {
	return m.c.do(ctx, http.MethodPut, "/admin0/mfa-required", mfaRequiredBody{Required: required}, nil)
}

// MFAAuthenticator is one enrolled authenticator in a per-user MFA config.
type MFAAuthenticator struct {
	ID         string `json:"id,omitempty"`
	Type       string `json:"type"`
	OOBChannel string `json:"oob_channel,omitempty"`
	Name       string `json:"name,omitempty"`
	Active     *bool  `json:"active,omitempty"`
}

// MFAUser is the per-user MFA configuration. User is filled in by the
// server on responses and ignored on requests.
type MFAUser struct {
	User           string             `json:"user,omitempty"`
	Required       *bool              `json:"required,omitempty"`
	Authenticators []MFAAuthenticator `json:"authenticators"`
}

func mfaUserPath(user string) string {
	return "/admin0/mfa/users/" + url.PathEscape(user)
}

// SetUser stores the MFA config for user and returns it as stored, with
// authenticator IDs and the active flag filled in.
func (m *MFAClient) SetUser(ctx context.Context, user string, u MFAUser) (MFAUser, error) {
	u.User = ""
	var out MFAUser
	if err := m.c.do(ctx, http.MethodPut, mfaUserPath(user), u, &out); err != nil {
		return MFAUser{}, err
	}
	return out, nil
}

// GetUser returns the MFA config for user, or an *APIError with status
// 404 when none is stored.
func (m *MFAClient) GetUser(ctx context.Context, user string) (MFAUser, error) {
	var out MFAUser
	if err := m.c.do(ctx, http.MethodGet, mfaUserPath(user), nil, &out); err != nil {
		return MFAUser{}, err
	}
	return out, nil
}

// DeleteUser removes the MFA config for user.
func (m *MFAClient) DeleteUser(ctx context.Context, user string) error {
	return m.c.do(ctx, http.MethodDelete, mfaUserPath(user), nil, nil)
}

// ListUsers returns every stored per-user MFA config, sorted by user.
func (m *MFAClient) ListUsers(ctx context.Context) ([]MFAUser, error) {
	var out []MFAUser
	if err := m.c.do(ctx, http.MethodGet, "/admin0/mfa/users", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
