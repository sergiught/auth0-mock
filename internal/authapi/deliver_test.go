package authapi

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMode(t *testing.T) {
	tests := []struct {
		mode        string
		issuesToken bool
		want        string
	}{
		{"", false, "query"},
		{"", true, "fragment"},
		{"query", true, "query"},
		{"fragment", false, "fragment"},
		{"form_post", false, "form_post"},
		{"web_message", false, "query"},
		{"web_message", true, "fragment"},
		{"bogus", false, "query"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, resolveMode(tc.mode, tc.issuesToken), "%q/%v", tc.mode, tc.issuesToken)
	}
}

func deliverTo(t *testing.T, redirect, mode string, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(redirect)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	deliver(w, u, mode, params)
	return w
}

func TestDeliver_Query(t *testing.T) {
	w := deliverTo(t, "https://app/cb?tenant=a", "query", url.Values{"state": {"a b&c#d"}, "code": {"x"}})
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "a", loc.Query().Get("tenant"))
	assert.Equal(t, "x", loc.Query().Get("code"))
	assert.Equal(t, "a b&c#d", loc.Query().Get("state"))
	assert.Empty(t, loc.Fragment)
}

func TestDeliver_Fragment(t *testing.T) {
	w := deliverTo(t, "https://app/cb?tenant=a#old", "fragment", url.Values{"state": {"a b&c#d"}, "code": {"x"}})
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, url.Values{"tenant": {"a"}}, loc.Query())
	frag, err := url.ParseQuery(loc.EscapedFragment())
	require.NoError(t, err)
	assert.Equal(t, "x", frag.Get("code"))
	assert.Equal(t, "a b&c#d", frag.Get("state"))
	assert.False(t, frag.Has("old"))
	assert.NotContains(t, w.Header().Get("Location"), "old")
}

func TestDeliver_FormPost(t *testing.T) {
	w := deliverTo(t, "https://app/cb?tenant=a", "form_post", url.Values{"state": {"<script>&\""}, "code": {"x"}})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	body := w.Body.String()
	assert.Contains(t, body, `method="post"`)
	assert.Contains(t, body, `action="`+html.EscapeString("https://app/cb?tenant=a")+`"`)
	assert.NotContains(t, body, "<script>")

	fields := map[string]string{}
	for _, m := range regexp.MustCompile(`name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		fields[html.UnescapeString(m[1])] = html.UnescapeString(m[2])
	}
	assert.Equal(t, "x", fields["code"])
	assert.Equal(t, "<script>&\"", fields["state"])
}
