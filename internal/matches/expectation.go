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
type Expectation struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Kind     Kind            `json:"-"`
	Request  *RequestMatcher `json:"request,omitempty"`
	Response ResponseDef     `json:"response"`
}

// RequestMatcher is the optional set of conditions an incoming request must
// satisfy for an Expectation to apply.
type RequestMatcher struct {
	Query map[string]string `json:"query,omitempty"`
	Body  json.RawMessage   `json:"body,omitempty"`
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
