package auth0mock

import (
	"context"
	"net/http"
)

// ClaimsClient owns the /admin0/claims surface — the per-process map
// of custom JWT claims merged into every minted access token and id
// token. Reach it via Client.Claims.
//
// Claims are global to the mock process: there's no per-subject or
// per-audience scoping at this layer (per-audience scopes/permissions
// live on Client.Permissions). For test isolation, call Clear from
// t.Cleanup.
type ClaimsClient struct {
	c *Client
}

// Get returns the current custom-claim map. An empty map (not nil) is
// returned when no claims are registered, matching the server's
// render.JSON behaviour on an empty store.
func (cl *ClaimsClient) Get(ctx context.Context) (map[string]any, error) {
	var resp map[string]any
	if err := cl.c.do(ctx, http.MethodGet, "/admin0/claims", nil, &resp); err != nil {
		return nil, err
	}
	if resp == nil {
		resp = map[string]any{}
	}
	return resp, nil
}

// Set replaces the entire claim map. Pass an empty map to clear
// without using Clear (semantically identical, one fewer round-trip
// if you already have the map).
func (cl *ClaimsClient) Set(ctx context.Context, claims map[string]any) error {
	if claims == nil {
		claims = map[string]any{}
	}
	return cl.c.do(ctx, http.MethodPut, "/admin0/claims", claims, nil)
}

// Clear removes every custom claim. Idempotent.
func (cl *ClaimsClient) Clear(ctx context.Context) error {
	return cl.c.do(ctx, http.MethodDelete, "/admin0/claims", nil, nil)
}
