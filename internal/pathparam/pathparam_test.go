package pathparam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reqWith(path, rawPath, param string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = path
	req.URL.RawPath = rawPath
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", param)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
}

func TestGet_DecodesWhenRawPathSet(t *testing.T) {
	got, err := Get(reqWith("/x/a|b", "/x/a%7Cb", "a%7Cb"), "id")
	require.NoError(t, err)
	assert.Equal(t, "a|b", got)
}

func TestGet_NoRawPathKeepsLiteralPercent(t *testing.T) {
	got, err := Get(reqWith("/x/100%", "", "100%"), "id")
	require.NoError(t, err)
	assert.Equal(t, "100%", got)
}

func TestGet_MalformedEscape(t *testing.T) {
	_, err := Get(reqWith("/x/%zz", "/x/%25zz", "%zz"), "id")
	require.Error(t, err)
}
