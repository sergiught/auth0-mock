package admin0

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
)

func authorizeErrorsDeps() Deps {
	d := newDeps()
	d.AuthorizeErrors = authorizeerr.NewStore()
	return d
}

func TestPostAuthorizeError_Registers(t *testing.T) {
	d := authorizeErrorsDeps()
	w := httptest.NewRecorder()
	body := `{"match":{"client_id":"a"},"error":"access_denied","error_description":"User cancelled"}`
	newRouter(d).ServeHTTP(w, httptest.NewRequest("POST", "/admin0/authorize-errors", strings.NewReader(body)))

	require.Equal(t, 201, w.Code)
	var got authorizeerr.Rule
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "access_denied", got.Error)
	assert.Equal(t, "User cancelled", got.ErrorDescription)
	assert.Equal(t, map[string]string{"client_id": "a"}, got.Match)

	found := d.AuthorizeErrors.Find(url.Values{"client_id": {"a"}})
	require.NotNil(t, found)
	assert.Equal(t, got.ID, found.ID)
}

func TestPostAuthorizeError_Rejects(t *testing.T) {
	tests := []struct{ name, body, msg string }{
		{"missing error", `{"match":{"a":"b"}}`, "error is required"},
		{"bad error chars", `{"error":"a\"b"}`, "error contains characters"},
		{"unknown field", `{"error":"x","description":"y"}`, "description"},
		{"malformed json", `{"error":`, "decode body"},
		{"match not strings", `{"error":"x","match":{"a":1}}`, "decode body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := authorizeErrorsDeps()
			w := httptest.NewRecorder()
			newRouter(d).ServeHTTP(w, httptest.NewRequest("POST", "/admin0/authorize-errors", strings.NewReader(tt.body)))

			require.Equal(t, 400, w.Code)
			assert.Contains(t, w.Body.String(), "invalid_body")
			assert.Contains(t, w.Body.String(), tt.msg)
			assert.Empty(t, d.AuthorizeErrors.List())
		})
	}
}

func TestListAuthorizeErrors(t *testing.T) {
	d := authorizeErrorsDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/authorize-errors", nil))
	require.Equal(t, 200, w.Code)
	assert.JSONEq(t, `{"rules":[]}`, w.Body.String())

	r1 := d.AuthorizeErrors.Add(authorizeerr.Rule{Error: "one"})
	r2 := d.AuthorizeErrors.Add(authorizeerr.Rule{Match: map[string]string{"k": "v"}, Error: "two"})

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/authorize-errors", nil))
	require.Equal(t, 200, w.Code)
	var got struct {
		Rules []authorizeerr.Rule `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, []authorizeerr.Rule{r1, r2}, got.Rules)
}

func TestDeleteAuthorizeErrors_All(t *testing.T) {
	d := authorizeErrorsDeps()
	d.AuthorizeErrors.Add(authorizeerr.Rule{Error: "one"})
	d.AuthorizeErrors.Add(authorizeerr.Rule{Error: "two"})

	w := httptest.NewRecorder()
	newRouter(d).ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/authorize-errors", nil))

	assert.Equal(t, 204, w.Code)
	assert.Empty(t, d.AuthorizeErrors.List())
}

func TestDeleteAuthorizeErrorByID(t *testing.T) {
	d := authorizeErrorsDeps()
	keep := d.AuthorizeErrors.Add(authorizeerr.Rule{Error: "keep"})
	drop := d.AuthorizeErrors.Add(authorizeerr.Rule{Error: "drop"})
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/authorize-errors/"+drop.ID, nil))
	assert.Equal(t, 204, w.Code)
	assert.Equal(t, []authorizeerr.Rule{keep}, d.AuthorizeErrors.List())

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/authorize-errors/nope", nil))
	assert.Equal(t, 204, w.Code)
	assert.Equal(t, []authorizeerr.Rule{keep}, d.AuthorizeErrors.List())
}

func TestReset_ClearsAuthorizeErrors(t *testing.T) {
	d := authorizeErrorsDeps()
	d.AuthorizeErrors.Add(authorizeerr.Rule{Error: "x"})

	w := httptest.NewRecorder()
	newRouter(d).ServeHTTP(w, httptest.NewRequest("POST", "/admin0/reset", nil))

	assert.Equal(t, 204, w.Code)
	assert.Empty(t, d.AuthorizeErrors.List())
}
