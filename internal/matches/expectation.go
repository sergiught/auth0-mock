// Package matches owns the in-memory store of registered mock expectations.
//
// An Expectation pairs an optional request matcher with a canned HTTP
// response, keyed by method and path. Paths can be concrete
// (e.g. "/api/v2/users/auth0|123") or OpenAPI templates
// (e.g. "/api/v2/users/{id}").
package matches

import (
	"encoding/json"
	"strings"
)

// Kind distinguishes how an Expectation's path is interpreted.
type Kind int

const (
	// KindExact means the registration was made with a concrete URL path.
	KindExact Kind = iota
	// KindTemplate means the registration was made with an OpenAPI path template.
	KindTemplate
)

// Expectation is a stored mock response, optionally conditioned on the
// incoming request via Request. A nil Request is a catch-all.
//
// ID is assigned by the store on Put() — callers leave it empty on
// registration and read it back from POST /admin0/expectations or
// GET /admin0/expectations. It uniquely identifies this expectation
// for the lifetime of the mock process; re-registering the same
// (method, path, matcher) tuple generates a fresh ID and supersedes
// the prior one.
//
// Hits is populated by List() and GetByID() from the store's per-ID
// atomic counter — every match served by Find() increments it by one.
// Callers leave Hits zero on Put(); the counter starts at 0 and is
// reset when the expectation is removed.
type Expectation struct {
	ID       string          `json:"id,omitempty"`
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Kind     Kind            `json:"-"`
	Request  *RequestMatcher `json:"request,omitempty"`
	Response ResponseDef     `json:"response"`
	Hits     int64           `json:"hits"`
}

// RequestMatcher is the optional set of conditions an incoming request must
// satisfy for an Expectation to apply.
//
// Headers are compared case-insensitively (canonical-MIME-header form), and
// subset-matched: every header in the matcher must be present with an equal
// value, but extra headers on the incoming request don't disqualify a match.
// Useful for stubbing different responses based on Authorization (Bearer vs
// DPoP), Accept-Language, Tenant-Id, etc.
type RequestMatcher struct {
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// ResponseDef is the canned response an Expectation returns.
type ResponseDef struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// KindOf reports whether path is a concrete URL (KindExact) or an OpenAPI
// path template containing a "{...}" segment (KindTemplate).
func KindOf(path string) Kind {
	if strings.ContainsAny(path, "{}") {
		return KindTemplate
	}
	return KindExact
}
