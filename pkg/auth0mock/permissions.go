package auth0mock

import (
	"context"
	"net/http"
	"net/url"
)

// PermissionsClient owns the /admin0/permissions surface — the
// per-audience permission lists that flow into the `permissions` claim
// of issued access tokens. Reach it via Client.Permissions.
//
// Audiences are arbitrary strings, often Auth0-style URL audiences
// (e.g. "https://api.example.com/"). The SDK url.PathEscape-encodes
// the audience before appending it to the path, so slashes, "?", and
// "#" characters all round-trip safely; the server's chi wildcard
// route URL-decodes on the way back.
type PermissionsClient struct {
	c *Client
}

// All returns the full per-audience permission map. Useful for
// snapshot-style assertions.
func (p *PermissionsClient) All(ctx context.Context) (map[string][]string, error) {
	var resp map[string][]string
	if err := p.c.do(ctx, http.MethodGet, "/admin0/permissions", nil, &resp); err != nil {
		return nil, err
	}
	if resp == nil {
		resp = map[string][]string{}
	}
	return resp, nil
}

// Clear removes every audience's permissions. Idempotent.
func (p *PermissionsClient) Clear(ctx context.Context) error {
	return p.c.do(ctx, http.MethodDelete, "/admin0/permissions", nil, nil)
}

// Get returns the permissions registered for audience. Returns an
// empty (non-nil) slice when the audience has no entry, matching the
// server's [] response shape.
func (p *PermissionsClient) Get(ctx context.Context, audience string) ([]string, error) {
	var resp []string
	if err := p.c.do(ctx, http.MethodGet, audiencePath(audience), nil, &resp); err != nil {
		return nil, err
	}
	if resp == nil {
		resp = []string{}
	}
	return resp, nil
}

// Set replaces the permissions for audience with the given slice.
// A nil or empty slice is encoded as `[]` on the wire (explicit clear,
// not a server-side no-op).
func (p *PermissionsClient) Set(ctx context.Context, audience string, perms []string) error {
	if perms == nil {
		perms = []string{}
	}
	return p.c.do(ctx, http.MethodPut, audiencePath(audience), perms, nil)
}

// Delete clears the permissions for one audience. Idempotent —
// deleting a never-set audience is a no-op (no error).
func (p *PermissionsClient) Delete(ctx context.Context, audience string) error {
	return p.c.do(ctx, http.MethodDelete, audiencePath(audience), nil, nil)
}

// audiencePath builds the per-audience subpath with the audience
// segment url.PathEscape-encoded. Centralised so every single-
// audience method (Get/Set/Delete) escapes the same way.
//
// Why PathEscape vs raw concatenation: chi's wildcard route accepts
// either form (it URL-decodes wildcards), but raw concatenation lets
// "?" and "#" in the audience be parsed as query/fragment by
// net/url before the request even leaves the SDK. Escaping defangs
// that without changing what the server sees.
func audiencePath(audience string) string {
	return "/admin0/permissions/" + url.PathEscape(audience)
}
