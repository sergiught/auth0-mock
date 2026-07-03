package auth0mock

import (
	"context"
	"net/http"
)

// ClaimMappingsClient owns the /admin0/claims/mappings surface — the
// request-parameter → claim-name map that /oauth/token projects into
// minted access tokens. Reach it via Client.Claims.Mappings.
//
// With a mapping registered (e.g. {"resource": "https://example.com/resource"}),
// a token request carrying that body parameter (form or JSON) mints a
// token whose mapped claim equals the parameter's value — per request,
// with precedence over the global Client.Claims overlay. Only mapped
// parameters are projected; the map is an allowlist. An empty map (the
// default) turns the projection off. For test isolation, call Clear
// from t.Cleanup.
type ClaimMappingsClient struct {
	c *Client
}

// Get returns the current mapping. An empty map (not nil) is returned
// when no mappings are registered. The server always renders an empty
// store as {} (MappingStore.Get hands render.JSON a non-nil map); the
// nil check below is a defensive normalization for any other body a
// transport or proxy might hand back, so callers never nil-check.
func (cl *ClaimMappingsClient) Get(ctx context.Context) (map[string]string, error) {
	var resp map[string]string
	if err := cl.c.do(ctx, http.MethodGet, "/admin0/claims/mappings", nil, &resp); err != nil {
		return nil, err
	}
	if resp == nil {
		resp = map[string]string{}
	}
	return resp, nil
}

// Set replaces the entire mapping. Keys are token-request body
// parameters, values are the claim names their values mint under. Pass
// an empty map to clear (semantically identical to Clear).
func (cl *ClaimMappingsClient) Set(ctx context.Context, mappings map[string]string) error {
	if mappings == nil {
		mappings = map[string]string{}
	}
	return cl.c.do(ctx, http.MethodPut, "/admin0/claims/mappings", mappings, nil)
}

// Clear removes every mapping. Idempotent.
func (cl *ClaimMappingsClient) Clear(ctx context.Context) error {
	return cl.c.do(ctx, http.MethodDelete, "/admin0/claims/mappings", nil, nil)
}
