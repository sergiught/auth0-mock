// Package mgmtapi mounts the spec-driven Management API surface onto
// chi, including each operation's /match and /reset siblings.
package mgmtapi

import (
	"strings"

	"github.com/sergiught/auth0-mock/internal/matches"
)

// Re-export so callers don't need to import matches just to talk about kinds.
const (
	KindExact    = matches.KindExact
	KindTemplate = matches.KindTemplate
)

// KindOfPath reports whether the path contains an OpenAPI template segment
// "{...}". Used to decide whether a /match registration is template-scoped or
// concrete-scoped.
func KindOfPath(p string) matches.Kind {
	if strings.ContainsAny(p, "{}") {
		return KindTemplate
	}
	return KindExact
}
