package spec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// ConciseSchemaError trims kin-openapi's verbose validation output to
// the actionable bits: a one-line `/json/pointer: reason` per failed
// field, joined with "; ". The raw error embeds a "Schema:\n  {…}\n
// Value:\n  …" block per failure, which gets relayed through JSON as
// `\n`-escaped soup and makes the wire response unreadable.
//
// Walks MultiError → SchemaError leaves. Falls back to the first line
// of err.Error() when the structure isn't a shape we know how to walk.
func ConciseSchemaError(err error) string {
	if err == nil {
		return ""
	}

	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		parts := make([]string, 0, len(multi))
		for _, e := range multi {
			if s := ConciseSchemaError(e); s != "" {
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

	return firstLine(err.Error())
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
