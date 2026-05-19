package mgmtapi

import (
	"errors"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/sergiught/auth0-mock/internal/spec"
)

// conciseValidationError trims kin-openapi's verbose validation output to
// the actionable bits: which field failed, why, and where. The raw
// kin-openapi message embeds a "Schema:\n  {…}\nValue:\n  …" block per
// failure, which gets relayed through JSON as `\n`-escaped soup and
// makes the wire response unreadable. This formatter:
//
//   - extracts a *openapi3filter.RequestError to identify the location
//     (parameter, request body, security);
//   - extracts every nested *openapi3.SchemaError (one per failed
//     field) and renders it as "/field/path: reason";
//   - joins multiple field errors with "; " so a multi-field-bad
//     payload still reports them all in one line.
//
// Falls back to the first line of err.Error() when the structure isn't
// one of the kin-openapi shapes we know how to walk.
func conciseValidationError(err error) string {
	if err == nil {
		return ""
	}

	// RequestError wraps the inner schema/parse error with locator info.
	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		inner := spec.ConciseSchemaError(reqErr.Err)
		if inner == "" {
			inner = spec.ConciseSchemaError(err)
		}
		switch {
		case reqErr.Parameter != nil:
			return fmt.Sprintf("parameter %q: %s", reqErr.Parameter.Name, inner)
		case reqErr.RequestBody != nil:
			return "request body: " + inner
		default:
			return inner
		}
	}

	return spec.ConciseSchemaError(err)
}
