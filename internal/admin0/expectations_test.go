package admin0_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
        "parameters":[
          {"name":"id","in":"path","required":true,"schema":{"type":"string"}},
          {"name":"fields","in":"query","required":false,"schema":{"type":"string"}}
        ],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}}}}}
      }
    },
    "/widgets":{
      "post":{
        "operationId":"createWidget",
        "requestBody":{
          "required":true,
          "content":{"application/json":{"schema":{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string"},"size":{"type":"integer"}}}}}
        },
        "responses":{"201":{"description":"created","content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}}}
      }
    },
    "/events":{
      "get":{
        "operationId":"subscribe_events",
        "responses":{
          "200":{"description":"stream","content":{"text/event-stream":{"schema":{"type":"object"}}}},
          "403":{"description":"forbidden"}
        }
      }
    }
  }
}`

func newExpectationsRouter(t *testing.T) (chi.Router, *matches.Store) {
	t.Helper()
	s, err := spec.Load([]byte(tinySpec))
	require.NoError(t, err)
	store := matches.NewStore()
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	r := chi.NewRouter()
	admin0.Mount(r, admin0.Deps{Matches: store, Validator: v})
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
		`{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"abc"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	m := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}", matches.MatchableRequest{})
	require.NotNil(t, m)
	assert.Equal(t, 200, m.Response.Status)
	assert.Equal(t, matches.KindExact, m.Kind)
	assert.Nil(t, m.Request)
}

func TestPostExpectation_TemplatePathIsTemplateKind(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/{id}","response":{"status":200,"body":{"id":"x"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	m := store.Find("GET", "/api/v2/widgets/anything", "/api/v2/widgets/{id}", matches.MatchableRequest{})
	require.NotNil(t, m)
	assert.Equal(t, matches.KindTemplate, m.Kind)
}

func TestPostExpectation_TemplatePathCanonicalised(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/{anything}","response":{"status":200,"body":{"id":"x"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	m := store.Find("GET", "/api/v2/widgets/whatever", "/api/v2/widgets/{id}", matches.MatchableRequest{})
	require.NotNil(t, m)
	assert.Equal(t, matches.KindTemplate, m.Kind)
}

func TestPostExpectation_RegistersRequestBodyMatcher(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"POST","path":"/api/v2/widgets","request":{"body":{"name":"w1"}},"response":{"status":201,"body":{"id":"w1"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	m := store.Find("POST", "/api/v2/widgets", "/api/v2/widgets",
		matches.MatchableRequest{Body: []byte(`{"name":"w1","size":3}`)})
	require.NotNil(t, m)
	require.NotNil(t, m.Request)

	// A request the matcher rejects yields no expectation.
	assert.Nil(t, store.Find("POST", "/api/v2/widgets", "/api/v2/widgets",
		matches.MatchableRequest{Body: []byte(`{"name":"other"}`)}))
}

func TestPostExpectation_RegistersQueryMatcher(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/{id}","request":{"query":{"fields":"id"}},"response":{"status":200,"body":{"id":"x"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	hit := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}",
		matches.MatchableRequest{Query: url.Values{"fields": {"id"}}})
	require.NotNil(t, hit)
	require.NotNil(t, hit.Request)

	miss := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}",
		matches.MatchableRequest{Query: url.Values{"fields": {"name"}}})
	assert.Nil(t, miss)
}

func TestPostExpectation_EmptyRequestMatcherIsCatchAll(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","request":{},"response":{"status":200,"body":{"id":"abc"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	m := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}", matches.MatchableRequest{})
	require.NotNil(t, m)
	assert.Nil(t, m.Request, "an empty request object must normalize to a nil catch-all")
}

func TestPostExpectation_Rejects(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	cases := []struct {
		name, body, wantCode string
		status               int
	}{
		{"missing response.status", `{"method":"GET","path":"/api/v2/widgets/abc","response":{}}`, "invalid_body", 400},
		{"missing method/path", `{"response":{"status":200,"body":{"id":"x"}}}`, "invalid_body", 400},
		{"unknown operation", `{"method":"GET","path":"/api/v2/nope","response":{"status":200,"body":{"id":"x"}}}`, "unknown_operation", 400},
		{"response schema violation", `{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":"not-an-object"}}`, "invalid_match", 400},
		{"undeclared status", `{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":418,"body":{"id":"x"}}}`, "invalid_match", 400},
		{"request matcher unknown field", `{"method":"POST","path":"/api/v2/widgets","request":{"body":{"hello":"hola"}},"response":{"status":201,"body":{"id":"x"}}}`, "invalid_request_match", 400},
		{"request matcher mistyped field", `{"method":"POST","path":"/api/v2/widgets","request":{"body":{"size":"big"}},"response":{"status":201,"body":{"id":"x"}}}`, "invalid_request_match", 400},
		{"request matcher unknown query param", `{"method":"GET","path":"/api/v2/widgets/abc","request":{"query":{"nope":"x"}},"response":{"status":200,"body":{"id":"x"}}}`, "invalid_request_match", 400},
		{"request body matcher on bodyless op", `{"method":"GET","path":"/api/v2/widgets/abc","request":{"body":{"id":"x"}},"response":{"status":200,"body":{"id":"x"}}}`, "invalid_request_match", 400},
		{"missing then status", `{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"x"}},"then":[{}]}`, "then[0].status required", 400},
		{"then schema violation", `{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"x"}},"then":[{"status":200,"body":{"id":"x"}},{"status":200,"body":"not-an-object"}]}`, "then[1]", 400},
		{"then undeclared status", `{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"x"}},"then":[{"status":418,"body":{"id":"x"}}]}`, "then[0]", 400},
		{"then success on event stream", `{"method":"GET","path":"/api/v2/events","response":{"status":403,"body":{"statusCode":403,"error":"Forbidden","message":"m"}},"then":[{"status":200}]}`, "then[0]", 400},
		{"success stub on event stream", `{"method":"GET","path":"/api/v2/events","response":{"status":200}}`, "POST /admin0/events", 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, r, http.MethodPost, "/admin0/expectations", c.body)
			assert.Equal(t, c.status, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), c.wantCode)
		})
	}
}

func TestPostExpectation_EventStreamAcceptsErrorStub(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/events","response":{"status":403,"body":{"statusCode":403,"error":"Forbidden","message":"Maximum concurrent event stream connections reached. Please close existing connections."}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	m := store.Find("GET", "/api/v2/events", "/api/v2/events", matches.MatchableRequest{})
	require.NotNil(t, m)
	assert.Equal(t, 403, m.Response.Status)
}

func TestPostExpectation_RequestMatcherAcceptsValidPartial(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	// "name" is required by the schema but a matcher is partial: a body with
	// only the optional "size" field must be accepted.
	rec := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"POST","path":"/api/v2/widgets","request":{"body":{"size":5}},"response":{"status":201,"body":{"id":"x"}}}`)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestListExpectations(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"abc"}}}`)
	rec := do(t, r, http.MethodGet, "/admin0/expectations", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(1), gjson.GetBytes(rec.Body.Bytes(), "expectations.#").Int())
	assert.Equal(t, "GET", gjson.GetBytes(rec.Body.Bytes(), "expectations.0.method").String())
	assert.Equal(t, "/api/v2/widgets/abc", gjson.GetBytes(rec.Body.Bytes(), "expectations.0.path").String())
	assert.Equal(t, int64(200), gjson.GetBytes(rec.Body.Bytes(), "expectations.0.response.status").Int())
}

func TestDeleteExpectations_One(t *testing.T) {
	r, store := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"abc"}}}`)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/xyz","response":{"status":200,"body":{"id":"xyz"}}}`)
	rec := do(t, r, http.MethodDelete, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Nil(t, store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}", matches.MatchableRequest{}))
	assert.NotNil(t, store.Find("GET", "/api/v2/widgets/xyz", "/api/v2/widgets/{id}", matches.MatchableRequest{}))
}

func TestDeleteExpectations_ClearsWholeOperationList(t *testing.T) {
	r, store := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"POST","path":"/api/v2/widgets","response":{"status":201,"body":{"id":"catchall"}}}`)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"POST","path":"/api/v2/widgets","request":{"body":{"name":"w1"}},"response":{"status":201,"body":{"id":"w1"}}}`)
	rec := do(t, r, http.MethodDelete, "/admin0/expectations", `{"method":"POST","path":"/api/v2/widgets"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Nil(t, store.Find("POST", "/api/v2/widgets", "/api/v2/widgets",
		matches.MatchableRequest{Body: []byte(`{"name":"w1"}`)}))
}

func TestDeleteExpectations_All(t *testing.T) {
	r, store := newExpectationsRouter(t)
	do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"abc"}}}`)
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

func TestGetExpectationByID(t *testing.T) {
	r, store := newExpectationsRouter(t)
	post := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"abc"}}}`)
	require.Equal(t, http.StatusCreated, post.Code, post.Body.String())
	require.Len(t, store.List(), 1)
	id := store.List()[0].ID
	require.NotEmpty(t, id)

	rec := do(t, r, http.MethodGet, "/admin0/expectations/"+id, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, id, gjson.GetBytes(rec.Body.Bytes(), "id").String())
	assert.Equal(t, "GET", gjson.GetBytes(rec.Body.Bytes(), "method").String())

	// Unknown id -> 404 unknown_id.
	rec = do(t, r, http.MethodGet, "/admin0/expectations/does-not-exist", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown_id")
}

func TestDeleteExpectationByID(t *testing.T) {
	r, store := newExpectationsRouter(t)
	post := do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"abc"}}}`)
	require.Equal(t, http.StatusCreated, post.Code, post.Body.String())
	require.Len(t, store.List(), 1)
	id := store.List()[0].ID

	rec := do(t, r, http.MethodDelete, "/admin0/expectations/"+id, "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Nil(t, store.GetByID(id))

	// Idempotent: deleting an unknown id is still 204.
	rec = do(t, r, http.MethodDelete, "/admin0/expectations/already-gone", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

const sequenceBody = `{"method":"GET","path":"/api/v2/widgets/abc","response":{"status":200,"body":{"id":"a"}},"then":[{"status":200,"body":{"id":"b"}}]}`

func TestPostExpectation_RegistersSequence(t *testing.T) {
	r, store := newExpectationsRouter(t)
	rec := do(t, r, http.MethodPost, "/admin0/expectations", sequenceBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	m := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}", matches.MatchableRequest{})
	require.NotNil(t, m)
	require.Len(t, m.Then, 1)
	assert.JSONEq(t, `{"id":"b"}`, string(m.Then[0].Body))
}

func TestPostExpectation_SequenceEchoedInList(t *testing.T) {
	r, _ := newExpectationsRouter(t)
	require.Equal(t, http.StatusCreated, do(t, r, http.MethodPost, "/admin0/expectations", sequenceBody).Code)
	require.Equal(t, http.StatusCreated, do(t, r, http.MethodPost, "/admin0/expectations",
		`{"method":"GET","path":"/api/v2/widgets/plain","response":{"status":200,"body":{"id":"p"}}}`).Code)

	rec := do(t, r, http.MethodGet, "/admin0/expectations", "")
	require.Equal(t, http.StatusOK, rec.Code)
	items := gjson.Get(rec.Body.String(), "expectations").Array()
	require.Len(t, items, 2)
	for _, it := range items {
		if it.Get("path").String() == "/api/v2/widgets/abc" {
			assert.Len(t, it.Get("then").Array(), 1)
		} else {
			assert.False(t, it.Get("then").Exists(), it.Raw)
		}
	}
}
