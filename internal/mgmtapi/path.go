// Package mgmtapi mounts the spec-driven Management API surface onto
// httprouter, including each operation's /match and /reset siblings.
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

// RouterPath translates an OpenAPI path template into the httprouter syntax.
// "{id}" -> ":id". httprouter requires a colon prefix for path params.
func RouterPath(openapiPath string) string {
	var b strings.Builder
	b.Grow(len(openapiPath))
	i := 0
	for i < len(openapiPath) {
		if openapiPath[i] == '{' {
			j := strings.IndexByte(openapiPath[i:], '}')
			if j > 0 {
				b.WriteByte(':')
				b.WriteString(openapiPath[i+1 : i+j])
				i += j + 1
				continue
			}
		}
		b.WriteByte(openapiPath[i])
		i++
	}
	return b.String()
}

// KindOfPath reports whether the path contains an OpenAPI template segment
// "{...}". Used to decide whether a /match registration is template-scoped or
// concrete-scoped.
func KindOfPath(p string) matches.Kind {
	if strings.ContainsAny(p, "{}") {
		return KindTemplate
	}
	return KindExact
}
