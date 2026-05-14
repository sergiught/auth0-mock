package admin0_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

const tinySpec = `{
  "openapi":"3.0.0",
  "info":{"title":"t","version":"1"},
  "servers":[{"url":"http://x/api/v2"}],
  "paths":{
    "/widgets/{id}":{
      "get":{
        "operationId":"getWidget",
        "parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}}}}}
      }
    }
  }
}`

func newExpectationsRouter(t *testing.T) (chi.Router, *matches.Store) {
	t.Helper()
	s, err := spec.Load([]byte(tinySpec))
	require.NoError(t, err)
	store := matches.NewStore()
	r := chi.NewRouter()
	admin0.Mount(r, admin0.Deps{Matches: store, Validator: spec.NewValidator(s)})
	return r, store
}

func do(t *testing.T, r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestPostExpectation_RegistersValid(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","status":200,"body":{"id":"abc"}}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	m := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}")
	require.NotNil(t, m)
	assert.Equal(t, 200, m.Status)
	assert.Equal(t, matches.KindExact, m.Kind)
}

func TestPostExpectation_TemplatePathIsTemplateKind(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/{id}","status":200,"body":{"id":"x"}}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	m := store.Find("GET", "/api/v2/widgets/anything", "/api/v2/widgets/{id}")
	require.NotNil(t, m)
	assert.Equal(t, matches.KindTemplate, m.Kind)
}

func TestPostExpectation_Rejects(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	cases := []struct {
		name, body, wantCode string
		status               int
	}{
		{"missing status", `{"method":"GET","path":"/api/v2/widgets/abc"}`, "invalid_body", 400},
		{"missing method/path", `{"status":200,"body":{"id":"x"}}`, "invalid_body", 400},
		{"unknown operation", `{"method":"GET","path":"/api/v2/nope","status":200,"body":{"id":"x"}}`, "unknown_operation", 400},
		{"schema violation", `{"method":"GET","path":"/api/v2/widgets/abc","status":200,"body":"not-an-object"}`, "invalid_match", 400},
		{"undeclared status", `{"method":"GET","path":"/api/v2/widgets/abc","status":418,"body":{"id":"x"}}`, "invalid_match", 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, r, http.MethodPost, "/admin0/expectations", c.body)
			assert.Equal(t, c.status, rec.Code)
			assert.Contains(t, rec.Body.String(), c.wantCode)
		})
	}
}

func TestListExpectations(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","status":200,"body":{"id":"abc"}}`)
	rec := do(t, r, http.MethodGet, "/admin0/expectations", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(1), gjson.GetBytes(rec.Body.Bytes(), "expectations.#").Int())
	assert.Equal(t, "GET", gjson.GetBytes(rec.Body.Bytes(), "expectations.0.method").String())
	assert.Equal(t, "/api/v2/widgets/abc", gjson.GetBytes(rec.Body.Bytes(), "expectations.0.path").String())
}

func TestDeleteExpectations_One(t *testing.T) {
	r, store := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","status":200,"body":{"id":"abc"}}`)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/xyz","status":200,"body":{"id":"xyz"}}`)
	rec := do(t, r, http.MethodDelete, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Nil(t, store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}"))
	assert.NotNil(t, store.Find("GET", "/api/v2/widgets/xyz", "/api/v2/widgets/{id}"))
}

func TestDeleteExpectations_All(t *testing.T) {
	r, store := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","status":200,"body":{"id":"abc"}}`)
	rec := do(t, r, http.MethodDelete, "/admin0/expectations", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Len(t, store.List(), 0)
}

func TestDeleteExpectations_Rejects(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	cases := []struct {
		name, body, wantCode string
		status               int
	}{
		{"malformed json", `not-json`, "invalid_body", 400},
		{"missing path", `{"method":"GET"}`, "invalid_body", 400},
		{"missing method", `{"path":"/api/v2/widgets/abc"}`, "invalid_body", 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, r, http.MethodDelete, "/admin0/expectations", c.body)
			assert.Equal(t, c.status, rec.Code)
			assert.Contains(t, rec.Body.String(), c.wantCode)
		})
	}
}

func TestDeleteExpectations_NonexistentIsNoop(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	rec := do(t, r, http.MethodDelete, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/never-registered"}`)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
