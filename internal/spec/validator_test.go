package spec

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minSpec = `{
  "openapi": "3.0.0",
  "info": {"title":"t","version":"1"},
  "servers": [{"url":"http://x/api/v2"}],
  "paths": {
    "/widgets/{id}": {
      "get": {
        "operationId": "getWidget",
        "parameters": [
          {"name":"id","in":"path","required":true,"schema":{"type":"string"}}
        ],
        "responses": {
          "200": {
            "description":"ok",
            "content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}}}
          }
        }
      },
      "post": {
        "operationId": "createWidget",
        "parameters": [
          {"name":"id","in":"path","required":true,"schema":{"type":"string"}},
          {"name":"fields","in":"query","required":false,"schema":{"type":"string"}}
        ],
        "requestBody": {
          "required": true,
          "content": {"application/json":{"schema":{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string"},"size":{"type":"integer"}}}}}
        },
        "responses": {"201":{"description":"created"}}
      }
    }
  }
}`

func loadMinSpec(t *testing.T) (*Spec, Operation, Operation) {
	t.Helper()
	s, err := Load([]byte(minSpec))
	require.NoError(t, err)
	var get, post Operation
	for op := range s.Operations() {
		if op.Method == "GET" {
			get = op
		} else {
			post = op
		}
	}
	require.NotNil(t, get.Op)
	require.NotNil(t, post.Op)
	return s, get, post
}

func TestValidator_ValidateRequest_Pass(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v2/widgets/abc", nil)
	err = v.ValidateRequest(req, getOp)
	assert.NoError(t, err)
}

func TestValidator_ValidateRequest_Fail(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v2/widgets/abc", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	err = v.ValidateRequest(req, postOp)
	assert.Error(t, err)
}

func TestValidator_ValidateRegistration_Pass(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"id": "abc"})
	err = v.ValidateRegistration(getOp, 200, body)
	assert.NoError(t, err)
}

func TestValidator_ValidateRegistration_FailsOnMissingField(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"unrelated": true})
	err = v.ValidateRegistration(getOp, 200, body)
	if assert.Error(t, err) {
		assert.True(t, strings.Contains(err.Error(), "id"))
	}
}

func TestValidator_ValidateRegistration_RejectsUndeclaredStatus(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"id": "abc"})
	err = v.ValidateRegistration(getOp, 999, body)
	assert.Error(t, err)
}

func TestValidatorResolve(t *testing.T) {
	s, err := Load([]byte(minSpec))
	require.NoError(t, err)
	v, err := NewValidator(s); require.NoError(t, err)

	// Concrete path resolves to the template operation.
	op, err := v.Resolve("GET", "/api/v2/widgets/abc")
	require.NoError(t, err)
	assert.Equal(t, "GET", op.Method)
	assert.Equal(t, "/api/v2/widgets/{id}", op.Template)
	require.NotNil(t, op.Op)
	assert.Equal(t, "getWidget", op.Op.OperationID)

	// A literal "{id}" segment resolves to the same operation.
	op, err = v.Resolve("GET", "/api/v2/widgets/{id}")
	require.NoError(t, err)
	assert.Equal(t, "/api/v2/widgets/{id}", op.Template)

	// Unknown path errors.
	_, err = v.Resolve("GET", "/api/v2/nonexistent")
	require.Error(t, err)

	// Unknown method on a known path errors.
	_, err = v.Resolve("DELETE", "/api/v2/widgets/abc")
	require.Error(t, err)
}

// silence unused-import linter for openapi3 in some Go versions.
var _ = openapi3.NewLoader

func TestValidator_ValidateRequestMatcher_RejectsUnknownField(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	err = v.ValidateRequestMatcher(postOp, json.RawMessage(`{"hello":"hola"}`))
	assert.Error(t, err)
}

func TestValidator_ValidateRequestMatcher_AcceptsValidPartial(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	// "name" is required by the schema, but a matcher is partial by design:
	// a body with only the optional "size" field must be accepted.
	err = v.ValidateRequestMatcher(postOp, json.RawMessage(`{"size":5}`))
	assert.NoError(t, err)
}

func TestValidator_ValidateRequestMatcher_RejectsMistypedKnownField(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	err = v.ValidateRequestMatcher(postOp, json.RawMessage(`{"size":"big"}`))
	assert.Error(t, err)
}

func TestValidator_ValidateRequestMatcher_EmptyBodyIsNoop(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	assert.NoError(t, v.ValidateRequestMatcher(postOp, nil))
	assert.NoError(t, v.ValidateRequestMatcher(postOp, json.RawMessage(`null`)))
}

func TestValidator_ValidateRequestMatcher_RejectsBodyForBodylessOperation(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	// The getWidget operation declares no request body; a body matcher cannot apply.
	err = v.ValidateRequestMatcher(getOp, json.RawMessage(`{"anything":1}`))
	assert.Error(t, err)
}

func TestValidator_ValidateQueryMatcher(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	assert.NoError(t, v.ValidateQueryMatcher(postOp, map[string]string{"fields": "name"}))
	assert.NoError(t, v.ValidateQueryMatcher(postOp, nil))

	err = v.ValidateQueryMatcher(postOp, map[string]string{"not_a_param": "x"})
	assert.Error(t, err)

	// "id" is a path parameter, not a query parameter, so it must be rejected.
	err = v.ValidateQueryMatcher(postOp, map[string]string{"id": "x"})
	assert.Error(t, err)
}

func TestValidator_ValidateRequestMatcher_AcceptsFullValidBody(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s); require.NoError(t, err)

	err = v.ValidateRequestMatcher(postOp, json.RawMessage(`{"name":"foo","size":5}`))
	assert.NoError(t, err)
}
