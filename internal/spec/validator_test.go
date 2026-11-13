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
          },
          "404": {"description":"not found"}
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

// Auth0's spec declares every error status without content, so an error
// body is checked against Auth0's error envelope instead.
func TestValidator_ValidateRegistration_ErrorEnvelope(t *testing.T) {
	s, getOp, postOp := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "empty body", body: ``},
		{name: "full envelope", body: `{"statusCode":404,"error":"Not Found","message":"gone","errorCode":"inexistent_widget"}`},
		{name: "errorCode optional", body: `{"statusCode":404,"error":"Not Found","message":"gone"}`},
		{name: "statusCode mismatch", body: `{"statusCode":403,"error":"Forbidden","message":"no"}`, wantErr: "statusCode 403 does not match status 404"},
		{name: "missing message", body: `{"statusCode":404,"error":"Not Found"}`, wantErr: "message"},
		{name: "unknown field", body: `{"statusCode":404,"error":"Not Found","mesage":"typo"}`, wantErr: "mesage"},
		{name: "statusCode not integer", body: `{"statusCode":"404","error":"Not Found","message":"gone"}`, wantErr: "statusCode"},
		{name: "not an object", body: `["gone"]`, wantErr: "object"},
		{name: "not JSON", body: `gone`, wantErr: "body json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := v.ValidateRegistration(getOp, 404, json.RawMessage(tc.body))
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}

	// A success status declared without content still takes no body:
	// the envelope is for errors only.
	err = v.ValidateRegistration(postOp, 201, json.RawMessage(`{"statusCode":201,"error":"Created","message":"ok"}`))
	assert.ErrorContains(t, err, "declares no body")
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
		keys    []string
		wantErr bool
	}{
		{name: "declared query param", keys: []string{"fields"}},
		{name: "nil map", keys: nil},
		{name: "undeclared param rejected", keys: []string{"not_a_param"}, wantErr: true},
		{
			// "id" is a path parameter, not a query parameter, so
			// it must be rejected.
			name:    "path param rejected as query",
			keys:    []string{"id"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateQueryMatcher(postOp, c.keys)
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
		    "id":"evt_aaaaaaaaaaaaaaaa000000",
		    "time":"2026-05-19T00:00:00Z",
		    "a0tenant":"my-tenant",
		    "a0stream":"est_aaaaaaaaaaaaaaaa000000",
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

func TestValidateRegistrationSkipping(t *testing.T) {
	s, err := Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := NewValidator(s)
	require.NoError(t, err)
	op, err := v.Resolve("POST", "/api/v2/users")
	require.NoError(t, err)

	t.Run("skipped_field_passes", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(op, 201, json.RawMessage(`{"email_verified":"sample"}`), []string{"/email_verified"})
		assert.NoError(t, err)
	})
	t.Run("nil_skip_fails", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(op, 201, json.RawMessage(`{"email_verified":"sample"}`), nil)
		assert.Error(t, err)
	})
	t.Run("other_errors_kept", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(op, 201, json.RawMessage(`{"email_verified":"sample","blocked":"no"}`), []string{"/email_verified"})
		assert.ErrorContains(t, err, "blocked")
	})
	t.Run("parent_pointer_covers_children", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(op, 201, json.RawMessage(`{"user_metadata":{"a":1},"email_verified":"x"}`), []string{"/email_verified", "/user_metadata"})
		assert.NoError(t, err)
	})
	t.Run("escaped_pointer", func(t *testing.T) {
		ok := skipCovers([]string{"/a~1b/c~0d"}, []string{"a/b", "c~d"})
		assert.True(t, ok)
		assert.False(t, skipCovers([]string{"/a"}, []string{"ab"}))
	})
	t.Run("error_envelope", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(op, 409, json.RawMessage(`{"statusCode":"x","error":"Conflict","message":"m"}`), []string{"/statusCode"})
		assert.NoError(t, err)
	})
}

// A templated error body is only known once rendered, so serve time
// checks the envelope the same way registration does.
func TestValidator_ValidateResponse_ErrorEnvelope(t *testing.T) {
	s, getOp, postOp := loadMinSpec(t)
	v, err := NewValidator(s)
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "empty body", body: ``},
		{name: "full envelope", body: `{"statusCode":404,"error":"Not Found","message":"gone","errorCode":"x"}`},
		{name: "statusCode rendered as string", body: `{"statusCode":"404","error":"Not Found","message":"gone"}`, wantErr: "statusCode"},
		{name: "statusCode mismatch", body: `{"statusCode":403,"error":"Forbidden","message":"no"}`, wantErr: "statusCode 403 does not match status 404"},
		{name: "missing message", body: `{"statusCode":404,"error":"Not Found"}`, wantErr: "message"},
		{name: "message not string", body: `{"statusCode":404,"error":"Not Found","message":7}`, wantErr: "message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := v.ValidateResponse(getOp, 404, nil, []byte(tc.body))
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}

	// Success statuses without content stay unchecked.
	assert.NoError(t, v.ValidateResponse(postOp, 201, nil, []byte(`{"anything":1}`)))
}

// A response schema that is a oneOf fails as a whole at the union's own
// pointer, which a skip pointer deeper inside does not cover. The skip
// must apply inside each branch instead.
func TestValidateRegistrationDeferring(t *testing.T) {
	s, err := Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := NewValidator(s)
	require.NoError(t, err)

	emailOp, err := v.Resolve("GET", "/api/v2/email-templates/{templateName}")
	require.NoError(t, err)
	usersOp, err := v.Resolve("POST", "/api/v2/users")
	require.NoError(t, err)
	groupsOp, err := v.Resolve("GET", "/api/v2/users/{id}/groups")
	require.NoError(t, err)
	logsOp, err := v.Resolve("GET", "/api/v2/log-streams")
	require.NoError(t, err)

	t.Run("text_defers_enum", func(t *testing.T) {
		body := json.RawMessage(`{"template":"sample"}`)
		assert.NoError(t, v.ValidateRegistrationDeferring(emailOp, 200, body, nil, []string{"/template"}))
		assert.Error(t, v.ValidateRegistrationDeferring(emailOp, 200, body, nil, nil))
	})
	t.Run("text_keeps_type_check", func(t *testing.T) {
		body := json.RawMessage(`{"email_verified":"x"}`)
		assert.Error(t, v.ValidateRegistrationDeferring(usersOp, 201, body, nil, []string{"/email_verified"}))
		assert.NoError(t, v.ValidateRegistrationDeferring(usersOp, 201, body, []string{"/email_verified"}, nil))
	})
	t.Run("all_of_branch_with_skipped_field", func(t *testing.T) {
		body := json.RawMessage(`[{"membership_created_at":""}]`)
		assert.NoError(t, v.ValidateRegistrationDeferring(groupsOp, 200, body, []string{"/0/membership_created_at"}, nil))
		assert.Error(t, v.ValidateRegistrationDeferring(groupsOp, 200, body, nil, nil))
		assert.Error(t, v.ValidateRegistrationDeferring(groupsOp, 200, json.RawMessage(`[{"membership_created_at":"","name":5}]`), []string{"/0/membership_created_at"}, nil))
	})
	t.Run("one_of_stays_exclusive", func(t *testing.T) {
		// Without a type, every log stream branch accepts the entry, and a
		// deferred name cannot pick one of them when served.
		body := json.RawMessage(`[{"name":"x","status":"active"}]`)
		assert.Error(t, v.ValidateRegistrationDeferring(logsOp, 200, body, nil, []string{"/0/name"}))
		assert.Error(t, v.ValidateRegistrationDeferring(logsOp, 200, json.RawMessage(`[{"name":"","status":"active"}]`), []string{"/0/name"}, nil))
		assert.NoError(t, v.ValidateRegistrationDeferring(logsOp, 200, json.RawMessage(`[{"type":"http","name":"x"}]`), nil, []string{"/0/name"}))
	})
	t.Run("one_of_decided_by_deferred_value", func(t *testing.T) {
		// The type itself is deferred: no branch accepts the sample as is,
		// so the served value is what picks one.
		body := json.RawMessage(`[{"type":"","name":"x"}]`)
		assert.NoError(t, v.ValidateRegistrationDeferring(logsOp, 200, body, []string{"/0/type"}, nil))
		assert.Error(t, v.ValidateRegistrationDeferring(logsOp, 200, body, nil, nil))
	})
}

func TestValidateRegistrationSkipping_Union(t *testing.T) {
	s, err := Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := NewValidator(s)
	require.NoError(t, err)

	usersOp, err := v.Resolve("GET", "/api/v2/users")
	require.NoError(t, err)
	logOp, err := v.Resolve("POST", "/api/v2/log-streams")
	require.NoError(t, err)

	t.Run("array_branch_with_skipped_field", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(usersOp, 200, json.RawMessage(`[{"email_verified":"sample"}]`), []string{"/0/email_verified"})
		assert.NoError(t, err)
	})
	t.Run("array_branch_without_skip_fails", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(usersOp, 200, json.RawMessage(`[{"email_verified":"sample"}]`), nil)
		assert.Error(t, err)
	})
	t.Run("genuine_mismatch_still_fails", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(usersOp, 200, json.RawMessage(`[{"email_verified":"sample","blocked":"no"}]`), []string{"/0/email_verified"})
		assert.Error(t, err)
		err = v.ValidateRegistrationSkipping(usersOp, 200, json.RawMessage(`[{"email_verified":"sample","name":5}]`), []string{"/0/email_verified"})
		assert.Error(t, err)
	})
	t.Run("skip_in_no_branch_shape_fails", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(usersOp, 200, json.RawMessage(`"text"`), []string{"/0/email_verified"})
		assert.Error(t, err)
	})
	t.Run("log_stream_union", func(t *testing.T) {
		err := v.ValidateRegistrationSkipping(logOp, 200, json.RawMessage(`{"type":"http","status":"sample"}`), []string{"/status"})
		assert.NoError(t, err)
		err = v.ValidateRegistrationSkipping(logOp, 200, json.RawMessage(`{"type":"http","status":"sample","name":5}`), []string{"/status"})
		assert.Error(t, err)
		err = v.ValidateRegistrationSkipping(logOp, 200, json.RawMessage(`{"type":"http","status":"sample"}`), nil)
		assert.Error(t, err)
	})
}
