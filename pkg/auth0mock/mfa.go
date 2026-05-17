package auth0mock

import (
	"context"
	"net/http"
)

// MFAClient owns the /admin0/mfa-required toggle — a single boolean
// that controls whether the password and password-realm grants demand
// an MFA step-up before issuing a token. Reach it via Client.MFA.
//
// The toggle is global to the mock process. There's no per-user or
// per-connection scoping at this layer; tests that want to flip MFA
// mid-flow simply call SetRequired before each phase.
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
