package spec

import (
	"slices"

	"github.com/getkin/kin-openapi/openapi3"
)

// scopeScheme is the security scheme that carries an operation's scopes.
const scopeScheme = "oAuth2ClientCredentials"

// OperationScopes returns the scopes an operation accepts (any one of them
// suffices), in spec order. Empty when the operation declares none.
func OperationScopes(op *openapi3.Operation) []string {
	if op == nil || op.Security == nil {
		return nil
	}
	var out []string
	for _, req := range *op.Security {
		for _, sc := range req[scopeScheme] {
			if !slices.Contains(out, sc) {
				out = append(out, sc)
			}
		}
	}
	return out
}

// ManagementScopes returns every scope any operation accepts, deduplicated and
// sorted. It is what a client_credentials token for the Management API gets
// when no narrower scope applies.
func (s *Spec) ManagementScopes() []string {
	seen := map[string]struct{}{}
	for op := range s.Operations() {
		for _, sc := range OperationScopes(op.Op) {
			seen[sc] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for sc := range seen {
		out = append(out, sc)
	}
	slices.Sort(out)
	return out
}
