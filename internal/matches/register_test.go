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

func TestPrepareTemplateOK(t *testing.T) {
	v := newValidator(t)
	body := []byte(`{"user_id":"auth0|{{uuid}}","email":"{{body.email}}","email_verified":"{{body.email_verified}}","created_at":"{{now}}","identities":[{"provider":"auth0","user_id":"{{uuid}}","connection":"{{body.connection}}","isSocial":false}]}`)
	exp, err := matches.Prepare(v, matches.Registration{
		Method: "POST", Path: "/api/v2/users",
		Response: matches.ResponseDef{
			Status: 201, Template: true, Body: body,
			Headers: map[string]string{"Location": "/api/v2/users/auth0|{{uuid}}"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, exp.Response.Tmpl())
	require.Equal(t, string(body), string(exp.Response.Body))
}

func TestPrepareTemplateErrors(t *testing.T) {
	v := newValidator(t)
	tests := []struct {
		name    string
		reg     matches.Registration
		code    string
		contain []string
	}{
		{"response", matches.Registration{Method: "GET", Path: "/api/v2/users/{id}",
			Response: matches.ResponseDef{Status: 200, Template: true, Body: []byte(`{"user_id":"{{nope}}"}`)}},
			"invalid_template", []string{"response", "nope"}},
		{"then", matches.Registration{Method: "GET", Path: "/api/v2/users/{id}",
			Response: matches.ResponseDef{Status: 404},
			Then:     []matches.ResponseDef{{Status: 200, Template: true, Body: []byte(`{"user_id":"{{nope}}"}`)}}},
			"invalid_template", []string{"then[0]", "nope"}},
		{"unknown path name", matches.Registration{Method: "GET", Path: "/api/v2/users/{id}",
			Response: matches.ResponseDef{Status: 200, Template: true, Body: []byte(`{"user_id":"{{path.nope}}"}`)}},
			"invalid_template", []string{"nope"}},
		{"schema format", matches.Registration{Method: "POST", Path: "/api/v2/users",
			Response: matches.ResponseDef{Status: 201, Template: true, Body: []byte(`{"created_at":"{{uuid}}"}`)}},
			"invalid_match", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := matches.Prepare(v, tc.reg)
			var re *matches.RegistrationError
			require.ErrorAs(t, err, &re)
			require.Equal(t, tc.code, re.Code, re.Message)
			for _, s := range tc.contain {
				require.Contains(t, re.Message, s)
			}
		})
	}
}

func TestPrepareTemplateConcretePath(t *testing.T) {
	v := newValidator(t)
	exp, err := matches.Prepare(v, matches.Registration{
		Method: "GET", Path: "/api/v2/users/auth0|1",
		Response: matches.ResponseDef{Status: 200, Template: true, Body: []byte(`{"user_id":"{{path.id}}"}`)},
	})
	require.NoError(t, err)
	require.NotNil(t, exp.Response.Tmpl())
}

func TestPrepareNoTemplateFlagKeepsBraces(t *testing.T) {
	v := newValidator(t)
	exp, err := matches.Prepare(v, matches.Registration{
		Method: "GET", Path: "/api/v2/email-templates/{templateName}",
		Response: matches.ResponseDef{Status: 200, Body: []byte(`{"template":"verify_email","body":"<p>{{ user.email }}</p>","from":"a@b.c","subject":"s","syntax":"liquid","enabled":true}`)},
	})
	require.NoError(t, err)
	require.Nil(t, exp.Response.Tmpl())
}

func TestPrepareThenSchemaMessage(t *testing.T) {
	v := newValidator(t)
	_, err := matches.Prepare(v, matches.Registration{
		Method: "GET", Path: "/api/v2/users/{id}",
		Response: matches.ResponseDef{Status: 404},
		Then:     []matches.ResponseDef{{Status: 200, Body: []byte(`{"user_id": 5}`)}},
	})
	var re *matches.RegistrationError
	require.ErrorAs(t, err, &re)
	require.Equal(t, "invalid_match", re.Code)
	require.True(t, strings.HasPrefix(re.Message, "registered then[0] violates schema for status 200: "), re.Message)
}

func TestPrepareTemplateThenOKAndSurvivesStore(t *testing.T) {
	v := newValidator(t)
	exp, err := matches.Prepare(v, matches.Registration{
		Method: "GET", Path: "/api/v2/users/{id}",
		Response: matches.ResponseDef{Status: 404},
		Then:     []matches.ResponseDef{{Status: 200, Template: true, Body: []byte(`{"user_id":"{{path.id}}"}`)}},
	})
	require.NoError(t, err)
	require.NotNil(t, exp.Then[0].Tmpl())

	s := matches.NewStore()
	s.Put(exp)
	found := s.Find("GET", "/api/v2/users/x", "/api/v2/users/{id}", matches.MatchableRequest{})
	require.NotNil(t, found)
	require.NotNil(t, found.Then[0].Tmpl())
}

// A required response field filled by a whole-string {{body.x}} must still
// register: the sample render keeps the slot instead of dropping the key.
func TestPrepareTemplateRequiredFieldFromBody(t *testing.T) {
	v := newValidator(t)
	reg := func(body string, tmpl bool) error {
		_, err := matches.Prepare(v, matches.Registration{
			Method: "POST", Path: "/api/v2/custom-domains/{id}/test",
			Response: matches.ResponseDef{Status: 200, Template: tmpl, Body: []byte(body)},
		})
		return err
	}
	require.Error(t, reg(`{}`, false), "success is required, so an empty body must fail")
	require.NoError(t, reg(`{"success":"{{body.success}}"}`, true))
}
