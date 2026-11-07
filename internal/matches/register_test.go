package matches_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

func newValidator(t *testing.T) *spec.Validator {
	t.Helper()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	return v
}

func TestPrepareErrors(t *testing.T) {
	v := newValidator(t)
	tests := []struct {
		name    string
		reg     matches.Registration
		message string // Exact, unless prefix is true.
		prefix  bool
		code    string
	}{
		{"empty method", matches.Registration{Path: "/api/v2/users"}, "method and path are required", false, "invalid_body"},
		{"status zero", matches.Registration{Method: "GET", Path: "/api/v2/users"}, "response.status is required", false, "invalid_body"},
		{"unknown operation", matches.Registration{Method: "GET", Path: "/api/v2/nope", Response: matches.ResponseDef{Status: 200}},
			"no Management API operation for GET /api/v2/nope", false, "unknown_operation"},
		{"events success", matches.Registration{Method: "GET", Path: "/api/v2/events", Response: matches.ResponseDef{Status: 200}},
			"GET /api/v2/events accepts only error stubs (status >= 400); push events with POST /admin0/events instead", false, "invalid_match"},
		{"response schema", matches.Registration{Method: "GET", Path: "/api/v2/users/{id}", Response: matches.ResponseDef{Status: 200, Body: []byte(`{"user_id": 5}`)}},
			"registered response violates schema for status 200: ", true, "invalid_match"},
		{"then status zero", matches.Registration{Method: "GET", Path: "/api/v2/users", Response: matches.ResponseDef{Status: 429}, Then: []matches.ResponseDef{{}}},
			"then[0].status is required", false, "invalid_body"},
		{"request body schema", matches.Registration{Method: "POST", Path: "/api/v2/users", Response: matches.ResponseDef{Status: 429},
			Request: &matches.RequestMatcher{Body: []byte(`{"email": 5}`)}},
			"request matcher body violates request schema: ", true, "invalid_request_match"},
		{"request query", matches.Registration{Method: "GET", Path: "/api/v2/users", Response: matches.ResponseDef{Status: 429},
			Request: &matches.RequestMatcher{Query: map[string]string{"bogus_param": "x"}}},
			"request matcher query is invalid: ", true, "invalid_request_match"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := matches.Prepare(v, tc.reg)
			var re *matches.RegistrationError
			require.ErrorAs(t, err, &re)
			require.Equal(t, tc.code, re.Code)
			if tc.prefix {
				require.True(t, strings.HasPrefix(re.Message, tc.message), re.Message)
			} else {
				require.Equal(t, tc.message, re.Message)
			}
		})
	}
}

func TestPrepareNormalises(t *testing.T) {
	v := newValidator(t)
	e, err := matches.Prepare(v, matches.Registration{
		Method:   "get",
		Path:     "/api/v2/users/{id}",
		Request:  &matches.RequestMatcher{},
		Response: matches.ResponseDef{Status: 404},
	})
	require.NoError(t, err)
	require.Equal(t, "GET", e.Method)
	require.Nil(t, e.Request)
	require.Equal(t, matches.KindTemplate, e.Kind)
	require.Equal(t, "/api/v2/users/{id}", e.Path)
}

func TestSameTarget(t *testing.T) {
	a := matches.Expectation{Method: "GET", Path: "/api/v2/users", Kind: matches.KindExact,
		Request: &matches.RequestMatcher{Query: map[string]string{"q": "a"}}}
	b := a
	require.True(t, matches.SameTarget(a, b))
	b.Request = &matches.RequestMatcher{Query: map[string]string{"q": "b"}}
	require.False(t, matches.SameTarget(a, b))
}

func TestPrepareTreatsEmptyMapsAsOmitted(t *testing.T) {
	v := newValidator(t)
	prepare := func(m *matches.RequestMatcher) matches.Expectation {
		t.Helper()
		e, err := matches.Prepare(v, matches.Registration{
			Method: "GET", Path: "/api/v2/users", Request: m, Response: matches.ResponseDef{Status: 429},
		})
		require.NoError(t, err)
		return e
	}
	withEmpty := &matches.RequestMatcher{Query: map[string]string{}, Headers: map[string]string{"X-A": "1"}}
	a := prepare(withEmpty)
	b := prepare(&matches.RequestMatcher{Headers: map[string]string{"X-A": "1"}})
	require.True(t, matches.SameTarget(a, b))
	require.Nil(t, a.Request.Query)
	require.NotNil(t, withEmpty.Query, "the caller's matcher must not be modified")

	c := prepare(&matches.RequestMatcher{Query: map[string]string{"q": "a"}, Headers: map[string]string{}})
	d := prepare(&matches.RequestMatcher{Query: map[string]string{"q": "a"}})
	require.True(t, matches.SameTarget(c, d))
	require.Nil(t, c.Request.Headers)
}
