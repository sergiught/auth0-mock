package mgmtapi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
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
		inner := conciseSchemaError(reqErr.Err)
		if inner == "" {
			// Inner wasn't a SchemaError/MultiError shape we recognise
			// — fall back to its own first line (NOT the outer wrapped
			// err, which would repeat the locator info we're about to
			// prepend ourselves).
			switch {
			case reqErr.Err != nil:
				inner = firstLine(reqErr.Err.Error())
			case reqErr.Reason != "":
				inner = firstLine(reqErr.Reason)
			default:
				inner = firstLine(err.Error())
			}
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

	if s := conciseSchemaError(err); s != "" {
		return s
	}
	return firstLine(err.Error())
}

// conciseSchemaError walks an error chain extracting the SchemaError
// leaves. Each leaf renders as `"/json/pointer": reason` (or just
// `reason` when the pointer is empty, e.g. a top-level type mismatch).
func conciseSchemaError(err error) string {
	if err == nil {
		return ""
	}

	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		parts := make([]string, 0, len(multi))
		for _, e := range multi {
			if s := conciseSchemaError(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	}

	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		reason := schemaErr.Reason
		if reason == "" {
			reason = firstLine(err.Error())
		}
		ptr := schemaErr.JSONPointer()
		if len(ptr) == 0 {
			return reason
		}
		return fmt.Sprintf("%q: %s", "/"+strings.Join(ptr, "/"), reason)
	}

	return ""
}

// firstLine returns the input up to (not including) the first newline,
// or the whole input when there's no newline. Used to strip the
// verbose Schema/Value blocks that kin-openapi appends after the
// real reason.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
