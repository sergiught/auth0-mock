package auth0mock

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// NamespaceHeader is the request header the mock reads to route a request
// to a namespace. It matches the server's header name.
const NamespaceHeader = "X-Auth0-Mock-Namespace"

var namespaceIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Namespace returns a client scoped to the isolated namespace id: it targets
// <base>/ns/<id>, so every sub-client (expectations, claims, reset, verify,
// journal, clock, events and the rest) only sees that namespace's state. The new
// client shares c's *http.Client but keeps its own verification ledger.
//
// Id must match [A-Za-z0-9_-]{1,64}. An already-namespaced client cannot be
// namespaced again. Namespace makes no request, so a server with namespaces
// off shows up at the first call.
//
// Namespace is the supported way to scope a client. A client built with
// NewClient(".../ns/abc") works for admin calls but does not know its id:
// NamespaceID is "" and Transport is a no-op.
//
// The client's BaseURL is <root>/ns/<id> (prefix mode). Tokens used against it,
// including the auth0mocktest.SubscribeEvents bearer, must be minted under that
// prefix. A token minted through Transport (header mode, default issuer) only
// works through Transport.
func (c *Client) Namespace(id string) (*Client, error) {
	if c.namespace != "" {
		return nil, fmt.Errorf("auth0mock: Namespace: client is already scoped to namespace %q", c.namespace)
	}
	if !namespaceIDRE.MatchString(id) {
		return nil, fmt.Errorf("auth0mock: Namespace: invalid id %q (want %s)", id, namespaceIDRE)
	}
	ns, err := NewClient(c.baseURL+"/ns/"+id, WithHTTPClient(c.http))
	if err != nil {
		return nil, err
	}
	ns.namespace = id
	return ns, nil
}

// NamespaceID returns the namespace the client is scoped to, or "" for a
// root client.
func (c *Client) NamespaceID() string { return c.namespace }

// DeleteNamespace removes the client's namespace and all its state. It is an
// error on a root client. A namespace the server does not know (404) is
// treated as already deleted.
func (c *Client) DeleteNamespace(ctx context.Context) error {
	if c.namespace == "" {
		return fmt.Errorf("auth0mock: DeleteNamespace: client is not namespaced")
	}
	// The lifecycle route is not itself namespaced, so build it from the
	// root URL.
	root := c.baseURL[:len(c.baseURL)-len("/ns/"+c.namespace)]
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, root+"/admin0/namespaces/"+c.namespace, http.NoBody)
	if err != nil {
		return fmt.Errorf("auth0mock: build DeleteNamespace request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("auth0mock: DELETE /admin0/namespaces/%s: %w", c.namespace, err)
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrainBytes)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	return nil
}

// Transport returns a RoundTripper that tags every request with the
// client's namespace header, so a system under test pointed at the mock's
// root URL is routed to this namespace. A nil base means
// http.DefaultTransport. On a root client base is returned unchanged. The
// caller's request is never mutated.
func (c *Client) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if c.namespace == "" {
		return base
	}
	return nsTransport{base: base, id: c.namespace}
}

type nsTransport struct {
	base http.RoundTripper
	id   string
}

func (t nsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set(NamespaceHeader, t.id)
	return t.base.RoundTrip(r)
}

// CloseIdleConnections passes through to the base transport when it supports
// it, so http.Client.CloseIdleConnections still reaches the real transport.
func (t nsTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
