// Package matches owns the in-memory store of registered mock responses.
//
// A Match is a canned HTTP response (status, headers, body) keyed by
// method and path. Paths can be either concrete (e.g. "/api/v2/users/auth0|123")
// or templates from the OpenAPI spec (e.g. "/api/v2/users/{id}").
package matches

import (
	"encoding/json"
	"strings"
)

// Kind distinguishes how a Match's path is interpreted.
type Kind int

const (
	// KindExact means the registration was made with a concrete URL path.
	KindExact Kind = iota
	// KindTemplate means the registration was made with an OpenAPI path template.
	KindTemplate
)

// Match is a stored mock response.
type Match struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Kind    Kind              `json:"-"`
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
