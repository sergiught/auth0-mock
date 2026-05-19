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

	"github.com/sergiught/auth0-mock/api"
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
	v, err := NewValidator(s)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v2/widgets/abc", nil)
	err = v.ValidateRequest(req, getOp)
	assert.NoError(t, err)
}

func TestValidator_ValidateRequest_Fail(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v2/widgets/abc", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	err = v.ValidateRequest(req, postOp)
	assert.Error(t, err)
}

func TestValidator_ValidateRegistration_Pass(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"id": "abc"})
	err = v.ValidateRegistration(getOp, 200, body)
	assert.NoError(t, err)
}

func TestValidator_ValidateRegistration_FailsOnMissingField(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"unrelated": true})
	err = v.ValidateRegistration(getOp, 200, body)
	if assert.Error(t, err) {
		assert.True(t, strings.Contains(err.Error(), "id"))
	}
}

func TestValidator_ValidateRegistration_RejectsUndeclaredStatus(t *testing.T) {
	s, getOp, _ := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"id": "abc"})
	err = v.ValidateRegistration(getOp, 999, body)
	assert.Error(t, err)
}

func TestValidatorResolve(t *testing.T) {
	s, err := Load([]byte(minSpec))
	require.NoError(t, err)
	v, err := NewValidator(s)
	require.NoError(t, err)

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

// TestValidator_ValidateRequestMatcher covers the matcher-side body
// validation: matchers are PARTIAL bodies (only the subset the operator
// wants to match on), so the validator rejects unknown fields + bad
// types but accepts a body missing schema-`required` fields.
func TestValidator_ValidateRequestMatcher(t *testing.T) {
	s, getOp, postOp := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	cases := []struct {
		name    string
		op      Operation
		body    json.RawMessage
		wantErr bool
	}{
		{
			name:    "rejects unknown field",
			op:      postOp,
			body:    json.RawMessage(`{"hello":"hola"}`),
			wantErr: true,
		},
		{
			// "name" is required by the schema, but a matcher is
			// partial by design — a body with only the optional
			// "size" field must be accepted.
			name: "accepts valid partial (missing required)",
			op:   postOp,
			body: json.RawMessage(`{"size":5}`),
		},
		{
			name:    "rejects mistyped known field",
			op:      postOp,
			body:    json.RawMessage(`{"size":"big"}`),
			wantErr: true,
		},
		{
			name: "empty body is a no-op (nil)",
			op:   postOp,
			body: nil,
		},
		{
			name: "empty body is a no-op (null)",
			op:   postOp,
			body: json.RawMessage(`null`),
		},
		{
			// GetWidget declares no request body; a body matcher
			// can't apply.
			name:    "rejects body for body-less operation",
			op:      getOp,
			body:    json.RawMessage(`{"anything":1}`),
			wantErr: true,
		},
		{
			name: "accepts full valid body",
			op:   postOp,
			body: json.RawMessage(`{"name":"foo","size":5}`),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateRequestMatcher(c.op, c.body)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidator_ValidateQueryMatcher covers the parameter-side rules:
// only declared query parameters are accepted; path parameters are
// rejected because they don't apply to query matchers.
func TestValidator_ValidateQueryMatcher(t *testing.T) {
	s, _, postOp := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	cases := []struct {
		name    string
		query   map[string]string
		wantErr bool
	}{
		{name: "declared query param", query: map[string]string{"fields": "name"}},
		{name: "nil map", query: nil},
		{name: "undeclared param rejected", query: map[string]string{"not_a_param": "x"}, wantErr: true},
		{
			// "id" is a path parameter, not a query parameter, so
			// it must be rejected.
			name:    "path param rejected as query",
			query:   map[string]string{"id": "x"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateQueryMatcher(postOp, c.query)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidator_ValidateEventStreamPayload(t *testing.T) {
	s, err := Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := NewValidator(s)
	require.NoError(t, err)
	op, err := v.Resolve("GET", "/api/v2/events")
	require.NoError(t, err)

	t.Run("valid_user_created", func(t *testing.T) {
		body := []byte(`{
		  "type":"user.created",
		  "offset":"0",
		  "event":{
		    "specversion":"1.0",
		    "type":"user.created",
		    "source":"https://auth0.local/",
		    "id":"evt_aaaaaaaaaaaaaaaa",
		    "time":"2026-05-19T00:00:00Z",
		    "a0tenant":"my-tenant",
		    "a0stream":"est_aaaaaaaaaaaaaaaa",
		    "data":{"object":{
		      "user_id":"u-1",
		      "email":"u@x.test",
		      "created_at":"2026-05-19T00:00:00Z",
		      "updated_at":"2026-05-19T00:00:00Z",
		      "identities":[]
		    }}
		  }
		}`)
		err := v.ValidateEventStreamPayload(op, 200, body)
		assert.NoError(t, err, "user.created event should match the oneOf: %v", err)
	})

	t.Run("missing_outer_type", func(t *testing.T) {
		body := []byte(`{"offset":"0","event":{}}`)
		err := v.ValidateEventStreamPayload(op, 200, body)
		assert.Error(t, err)
	})

	t.Run("unknown_outer_type", func(t *testing.T) {
		body := []byte(`{"type":"not.a.real.event","offset":"0","event":{}}`)
		err := v.ValidateEventStreamPayload(op, 200, body)
		assert.Error(t, err)
	})

	t.Run("non_json_body", func(t *testing.T) {
		err := v.ValidateEventStreamPayload(op, 200, []byte("not json"))
		assert.Error(t, err)
	})
}
