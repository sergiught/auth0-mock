package mgmtapi

import (
	"errors"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/stretchr/testify/assert"
)

func TestConciseValidationError_PatternFailureExtractsFieldAndReason(t *testing.T) {
	t.Parallel()
	// Simulate the realistic kin-openapi shape that surfaces on a pattern
	// mismatch — verbose body intentionally included to verify it gets
	// stripped down to the reason only.
	schemaErr := &openapi3.SchemaError{
		Reason:      `string doesn't match the regular expression "^[-A-Fa-f0-9]+$"`,
		SchemaField: "pattern",
	}
	wrapped := &openapi3filter.RequestError{
		Reason:      "request body has an error",
		Err:         schemaErr,
		RequestBody: &openapi3.RequestBody{Required: true},
	}
	got := conciseValidationError(wrapped)
	assert.Contains(t, got, "request body")
	assert.Contains(t, got, `string doesn't match the regular expression "^[-A-Fa-f0-9]+$"`)
	assert.NotContains(t, got, "Schema:", "verbose Schema block must be trimmed")
	assert.NotContains(t, got, "Value:", "verbose Value block must be trimmed")
}

func TestConciseValidationError_ParameterErrorMentionsParameterName(t *testing.T) {
	t.Parallel()
	param := &openapi3.Parameter{Name: "page", In: "query"}
	wrapped := &openapi3filter.RequestError{
		Parameter: param,
		Err:       errors.New("value is not a number"),
	}
	got := conciseValidationError(wrapped)
	assert.Equal(t, `parameter "page": value is not a number`, got)
}

func TestConciseValidationError_MultiErrorJoinsAll(t *testing.T) {
	t.Parallel()
	multi := openapi3.MultiError{
		&openapi3.SchemaError{Reason: "field a missing", SchemaField: "required"},
		&openapi3.SchemaError{Reason: "field b too short", SchemaField: "minLength"},
	}
	got := conciseValidationError(multi)
	assert.Contains(t, got, "field a missing")
	assert.Contains(t, got, "field b too short")
	assert.Contains(t, got, ";", "multiple field errors must be joined with a separator")
}

func TestConciseValidationError_UnknownShapeFallsBackToFirstLine(t *testing.T) {
	t.Parallel()
	err := errors.New("plain old error\nwith a verbose\nstack trace")
	got := conciseValidationError(err)
	assert.Equal(t, "plain old error", got)
}

func TestConciseValidationError_NilReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", conciseValidationError(nil))
}

// TestConciseValidationError_VerboseSchemaShapeIsTrimmed exercises the
// real-world failure the user reported: kin-openapi appends a multi-line
// "Schema:\n  {…}\nValue:\n  …" block to the SchemaError. The formatter
// must reduce that to one actionable line; relying on .Reason / .JSONPointer
// rather than .Error() is the trick.
func TestConciseValidationError_VerboseSchemaShapeIsTrimmed(t *testing.T) {
	t.Parallel()

	// Build a SchemaError that carries the same trailing verbosity as the
	// real kin-openapi failure. The pointer/reason fields are what we
	// read; .Value + .Schema fill in the .Error() multi-line output.
	schemaErr := &openapi3.SchemaError{
		Reason:      `string doesn't match the regular expression "^[-A-Fa-f0-9]+$"`,
		SchemaField: "pattern",
		Schema: &openapi3.Schema{
			Type:      &openapi3.Types{"string"},
			Pattern:   "^[-A-Fa-f0-9]+$",
			MaxLength: ptrUint64(36),
		},
		Value: "",
	}
	wrapped := &openapi3filter.RequestError{
		Reason:      "request body has an error",
		Err:         schemaErr,
		RequestBody: &openapi3.RequestBody{Required: true},
	}

	got := conciseValidationError(wrapped)
	assert.Equal(t,
		`request body: string doesn't match the regular expression "^[-A-Fa-f0-9]+$"`,
		got)
	// No leftover noise from the verbose Schema/Value blocks.
	assert.NotContains(t, got, "Schema:")
	assert.NotContains(t, got, "Value:")
	assert.NotContains(t, got, "\n")
}

func ptrUint64(v uint64) *uint64 { return &v }
