package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func corsOK(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORS_InvalidEntries(t *testing.T) {
	for _, e := range []string{
		"localhost:3000", "https://", "https://app.test/path",
		"https://app.test?q=1", "https://a.*.com", "https://*foo.com", "https://app.*",
		"https://u@app.test", "https://app.test:",
	} {
		_, err := CORS([]string{e})
		require.Error(t, err, e)
		assert.Contains(t, err.Error(), e)
	}
	for _, e := range []string{"*", "https://*.example.com", "http://localhost:3000/"} {
		_, err := CORS([]string{e})
		require.NoError(t, err, e)
	}
}

func TestCORS_OriginMatching(t *testing.T) {
	cases := []struct {
		allowed []string
		origin  string
		want    bool
	}{
		{nil, "http://localhost:3000", true},
		{nil, "null", true},
		{[]string{"*"}, "https://x.test", true},
		{[]string{"http://localhost:3000"}, "http://localhost:3000", true},
		{[]string{"http://localhost:3000"}, "http://localhost:3001", false},
		{[]string{"HTTP://LocalHost:3000/"}, "http://localhost:3000", true},
		{[]string{"https://*.example.com"}, "https://app.example.com", true},
		{[]string{"https://*.example.com"}, "https://a.b.example.com", false},
		{[]string{"https://*.example.com"}, "https://example.com", false},
		{[]string{"https://*.example.com"}, "http://app.example.com", false},
		{[]string{"https://*.example.com"}, "https://app.example.com:8443", false},
		{[]string{"https://*.example.com:8443"}, "https://app.example.com:8443", true},
		{[]string{"http://localhost:3000"}, "null", false},
		{[]string{"https://app.test:443"}, "https://app.test", true},
		{[]string{"http://*.example.com:80"}, "http://a.example.com", true},
		{[]string{"http://localhost:3000", "https://*.example.com"}, "http://localhost:3000", true},
		{[]string{"http://localhost:3000", "https://*.example.com"}, "https://app.example.com", true},
	}
	for _, c := range cases {
		mw, err := CORS(c.allowed)
		require.NoError(t, err)
		called := false
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/x", nil)
		r.Header.Set("Origin", c.origin)
		mw(corsOK(&called)).ServeHTTP(w, r)
		assert.True(t, called, "%v %s", c.allowed, c.origin)
		assert.Equal(t, 200, w.Code)
		if c.want {
			assert.Equal(t, c.origin, w.Header().Get("Access-Control-Allow-Origin"), "%v %s", c.allowed, c.origin)
			assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		} else {
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), "%v %s", c.allowed, c.origin)
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
		}
	}
}

func TestCORS_NoOriginUntouched(t *testing.T) {
	mw, err := CORS(nil)
	require.NoError(t, err)
	called := false
	w := httptest.NewRecorder()
	mw(corsOK(&called)).ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	assert.True(t, called)
	for k := range w.Header() {
		assert.NotContains(t, k, "Access-Control-")
	}
	assert.Empty(t, w.Header().Values("Vary"))
}

func preflight(origin string, reqHeaders string) *http.Request {
	r := httptest.NewRequest("OPTIONS", "/x", nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	if reqHeaders != "" {
		r.Header.Set("Access-Control-Request-Headers", reqHeaders)
	}
	return r
}

func TestCORS_Preflight(t *testing.T) {
	mw, err := CORS([]string{"http://localhost:3000"})
	require.NoError(t, err)
	called := false
	w := httptest.NewRecorder()
	mw(corsOK(&called)).ServeHTTP(w, preflight("http://localhost:3000", "authorization, content-type"))
	assert.Equal(t, 204, w.Code)
	assert.False(t, called)
	h := w.Header()
	assert.Equal(t, "GET, POST, PUT, PATCH, DELETE, OPTIONS", h.Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "authorization, content-type", h.Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "86400", h.Get("Access-Control-Max-Age"))
	assert.Equal(t, "http://localhost:3000", h.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", h.Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, h.Values("Vary"), "Origin")
	assert.Contains(t, h.Values("Vary"), "Access-Control-Request-Method")
	assert.Contains(t, h.Values("Vary"), "Access-Control-Request-Headers")

	w = httptest.NewRecorder()
	mw(corsOK(&called)).ServeHTTP(w, preflight("http://localhost:3000", ""))
	assert.Equal(t, 204, w.Code)
	_, present := w.Header()["Access-Control-Allow-Headers"]
	assert.False(t, present)
}

func TestCORS_PreflightDisallowed(t *testing.T) {
	mw, err := CORS([]string{"http://localhost:3000"})
	require.NoError(t, err)
	called := false
	w := httptest.NewRecorder()
	mw(corsOK(&called)).ServeHTTP(w, preflight("http://evil.test", "authorization"))
	assert.Equal(t, 403, w.Code)
	assert.Empty(t, w.Body.String())
	assert.False(t, called)
	for k := range w.Header() {
		assert.NotContains(t, k, "Access-Control-")
	}
	assert.Contains(t, w.Header().Values("Vary"), "Origin")
}

func TestCORS_DisallowedSimpleRequestVaries(t *testing.T) {
	mw, err := CORS([]string{"http://localhost:3000"})
	require.NoError(t, err)
	called := false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "http://evil.test")
	mw(corsOK(&called)).ServeHTTP(w, r)
	assert.True(t, called)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Values("Vary"), "Origin")
}

func TestCORS_UppercaseOriginEchoedVerbatim(t *testing.T) {
	mw, err := CORS([]string{"http://localhost:3000"})
	require.NoError(t, err)
	called := false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "HTTP://LOCALHOST:3000")
	mw(corsOK(&called)).ServeHTTP(w, r)
	assert.Equal(t, "HTTP://LOCALHOST:3000", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_OptionsWithoutRequestMethodPassesThrough(t *testing.T) {
	mw, err := CORS(nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/x", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})).ServeHTTP(w, r)
	assert.Equal(t, 405, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_VaryIsAppended(t *testing.T) {
	mw, err := CORS(nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
	})).ServeHTTP(w, r)
	assert.Contains(t, w.Header().Values("Vary"), "Origin")
	assert.Contains(t, w.Header().Values("Vary"), "Accept-Encoding")
}

func TestCORS_HeadersOnErrorStatus(t *testing.T) {
	mw, err := CORS(nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})).ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
}
