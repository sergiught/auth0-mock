package admin0

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/matches"
)

func newRouter(store *matches.Store) *httprouter.Router {
	r := httprouter.New()
	Mount(r, store)
	return r
}

func TestReset_WipesAllMatches(t *testing.T) {
	store := matches.NewStore()
	store.Put(matches.Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate, Status: 200})

	r := newRouter(store)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/admin0/reset", nil))

	assert.Equal(t, 204, w.Code)
	assert.Empty(t, store.List())
}

func TestMatches_ReturnsRegisteredMatches(t *testing.T) {
	store := matches.NewStore()
	store.Put(matches.Match{
		Method: "GET", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate,
		Status: 200, Body: json.RawMessage(`{"x":1}`),
	})

	r := newRouter(store)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/matches", nil))

	require.Equal(t, 200, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.True(t, strings.Contains(w.Body.String(), `"path":"/api/v2/users/{id}"`))
}
