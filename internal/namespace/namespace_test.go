package namespace_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/namespace"
)

type echo struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Raw     string `json:"raw"`
	Escaped string `json:"escaped"`
	Issuer  string `json:"issuer"`
}

type fixture struct {
	d      *namespace.Dispatcher
	builds atomic.Int32
	mu     sync.Mutex
	closes map[string]int
	fail   atomic.Bool
}

func newFixture(limit int) *fixture {
	f := &fixture{closes: map[string]int{}}
	def := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(echo{ID: "default", Path: r.URL.Path})
	})
	f.d = namespace.New(def, func(id string) (*namespace.Instance, error) {
		if f.fail.Load() {
			return nil, errors.New("boom")
		}
		f.builds.Add(1)
		return &namespace.Instance{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(echo{
					ID: id, Path: r.URL.Path, Raw: r.URL.RawPath, Escaped: r.URL.EscapedPath(),
					Issuer: jwks.ResolveIssuer("https://mock/", r),
				})
			}),
			Close: func(context.Context) {
				f.mu.Lock()
				defer f.mu.Unlock()
				f.closes[id]++
			},
		}, nil
	}, limit)
	return f
}

func (f *fixture) do(method, path, header string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if header != "" {
		r.Header.Set(namespace.Header, header)
	}
	w := httptest.NewRecorder()
	f.d.ServeHTTP(w, r)
	return w
}

func (f *fixture) echo(t *testing.T, path, header string) echo {
	t.Helper()
	w := f.do(http.MethodGet, path, header)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var e echo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e))
	return e
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var m struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
	return m.ErrorCode
}

func TestDispatcher_DefaultUntouched(t *testing.T) {
	f := newFixture(10)
	e := f.echo(t, "/oauth/token", "")
	assert.Equal(t, "default", e.ID)
	assert.Zero(t, f.builds.Load())
}

func TestDispatcher_HeaderRoutes(t *testing.T) {
	f := newFixture(10)
	e := f.echo(t, "/oauth/token", "abc")
	assert.Equal(t, "abc", e.ID)
	assert.Equal(t, "/oauth/token", e.Path)
	assert.Equal(t, "https://mock/", e.Issuer)
}

func TestDispatcher_PrefixRoutes(t *testing.T) {
	f := newFixture(10)
	e := f.echo(t, "/ns/abc/oauth/token", "")
	assert.Equal(t, "abc", e.ID)
	assert.Equal(t, "/oauth/token", e.Path)
	assert.Equal(t, "https://mock/ns/abc/", e.Issuer)
	assert.Equal(t, "/", f.echo(t, "/ns/abc", "").Path)
}

func TestDispatcher_HeaderAndPrefix(t *testing.T) {
	f := newFixture(10)
	assert.Equal(t, "abc", f.echo(t, "/ns/abc/oauth/token", "abc").ID)
	w := f.do(http.MethodGet, "/ns/abc/oauth/token", "other")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_namespace", errCode(t, w))
}

func TestDispatcher_InvalidID(t *testing.T) {
	f := newFixture(10)
	for _, c := range []struct{ path, header string }{
		{"/ns/a.b/x", ""},
		{"/ns/" + strings.Repeat("a", 65) + "/x", ""},
		{"/ns/", ""},
		{"/ns//x", ""},
		{"/x", "bad id"},
	} {
		w := f.do(http.MethodGet, c.path, c.header)
		assert.Equal(t, http.StatusBadRequest, w.Code, c.path)
		assert.Equal(t, "invalid_namespace", errCode(t, w), c.path)
	}
	assert.Zero(t, f.builds.Load())
}

func TestDispatcher_PrefixKeepsEscapedPath(t *testing.T) {
	f := newFixture(10)
	e := f.echo(t, "/ns/a/api/v2/users/auth0%7C123", "")
	assert.Equal(t, "/api/v2/users/auth0|123", e.Path)
	assert.Equal(t, "/api/v2/users/auth0%7C123", e.Escaped)
	e = f.echo(t, "/ns/a/api/v2/users/a%2Fb", "")
	assert.Equal(t, "/api/v2/users/a/b", e.Path)
	assert.Equal(t, "/api/v2/users/a%2Fb", e.Raw)
}

func TestDispatcher_EncodedPrefixKeepsPathConsistent(t *testing.T) {
	f := newFixture(10)
	e := f.echo(t, "/%6Es/a/x%2Fy", "")
	assert.Equal(t, "a", e.ID)
	assert.Equal(t, "/x/y", e.Path)
	assert.Contains(t, []string{"", "/x%2Fy"}, e.Raw)
	assert.Equal(t, "/x/y", e.Escaped)
}

func TestDispatcher_ConcurrentCreateBuildsOnce(t *testing.T) {
	f := newFixture(10)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.do(http.MethodGet, "/x", "x")
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, f.builds.Load())
}

func TestDispatcher_Cap(t *testing.T) {
	f := newFixture(2)
	f.echo(t, "/x", "a")
	f.echo(t, "/x", "b")
	w := f.do(http.MethodGet, "/x", "c")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "too_many_namespaces", errCode(t, w))
	assert.Contains(t, w.Body.String(),
		"namespace limit reached (NAMESPACE_MAX=2); delete unused namespaces with DELETE /admin0/namespaces/{id}")
	assert.Equal(t, "a", f.echo(t, "/x", "a").ID, "existing namespaces keep working")
	assert.Equal(t, http.StatusNoContent, f.do(http.MethodDelete, "/admin0/namespaces/a", "").Code)
	assert.Equal(t, "c", f.echo(t, "/x", "c").ID)
}

func TestDispatcher_Lifecycle(t *testing.T) {
	f := newFixture(10)
	f.echo(t, "/x", "b")
	f.echo(t, "/x", "a")

	w := f.do(http.MethodGet, "/admin0/namespaces", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list []struct {
		ID         string `json:"id"`
		CreatedAt  string `json:"created_at"`
		LastUsedAt string `json:"last_used_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "a", list[0].ID)
	assert.Equal(t, "b", list[1].ID)
	for _, l := range list {
		_, err := time.Parse(time.RFC3339, l.CreatedAt)
		require.NoError(t, err)
		_, err = time.Parse(time.RFC3339, l.LastUsedAt)
		require.NoError(t, err)
	}

	assert.Equal(t, http.StatusNoContent, f.do(http.MethodDelete, "/admin0/namespaces/a", "").Code)
	assert.Equal(t, 1, f.closes["a"])
	w = f.do(http.MethodDelete, "/admin0/namespaces/a", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "namespace_not_found", errCode(t, w))

	before := f.builds.Load()
	w = f.do(http.MethodDelete, "/admin0/namespaces/zz", "zz")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, before, f.builds.Load())
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, "/admin0/namespaces", "zz").Code)
	assert.Equal(t, before, f.builds.Load())

	assert.Equal(t, http.StatusMethodNotAllowed, f.do(http.MethodPost, "/admin0/namespaces", "").Code)

	assert.Equal(t, http.StatusNoContent, f.do(http.MethodDelete, "/admin0/namespaces", "").Code)
	assert.Equal(t, 1, f.closes["b"])
	w = f.do(http.MethodGet, "/admin0/namespaces", "")
	assert.JSONEq(t, "[]", w.Body.String())
}

func TestDispatcher_FactoryError(t *testing.T) {
	f := newFixture(10)
	f.fail.Store(true)
	w := f.do(http.MethodGet, "/x", "a")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "namespace_build_failed", errCode(t, w))
	f.fail.Store(false)
	assert.Equal(t, "a", f.echo(t, "/x", "a").ID)
	assert.EqualValues(t, 1, f.builds.Load())
}

func TestDispatcher_HealthShared(t *testing.T) {
	f := newFixture(10)
	assert.Equal(t, "default", f.echo(t, "/ns/q/healthz", "").ID)
	assert.Equal(t, "default", f.echo(t, "/readyz", "q").ID)
	assert.Zero(t, f.builds.Load())
}
