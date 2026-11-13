package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postUserinfo(r chi.Router, header, form string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/userinfo", strings.NewReader(form))
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUserinfo_PostAcceptsToken(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	tok := accessTokenOf(t, exchangeCode(r, code, ""))

	for name, w := range map[string]*httptest.ResponseRecorder{
		"header":    postUserinfo(r, "Bearer "+tok, ""),
		"form body": postUserinfo(r, "", url.Values{"access_token": {tok}}.Encode()),
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "alice@example.com", body["sub"])
			assert.Equal(t, "alice", body["nickname"])
		})
	}
}

func TestUserinfo_PostBothMethodsIsInvalidRequest(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	w := postUserinfo(r, "Bearer abc", url.Values{"access_token": {"abc"}}.Encode())
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"invalid_request","error_description":"access token must be sent using only one method"}`, w.Body.String())
}

func TestUserinfo_PostWithoutTokenIsUnauthorized(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	w := postUserinfo(r, "", "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_token")
	w = postUserinfo(r, "", url.Values{"other": {"x"}}.Encode())
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserinfo_PostMalformedBodyIsInvalidRequest(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	w := postUserinfo(r, "", "access_token=%zz")
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
}

func TestUserinfo_PostBasicHeaderDoesNotConflictWithFormToken(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	tok := accessTokenOf(t, exchangeCode(r, code, ""))
	w := postUserinfo(r, "Basic dTpw", url.Values{"access_token": {tok}}.Encode())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "alice@example.com")
}

func TestUserinfo_PostIgnoresMalformedQueryString(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	tok := accessTokenOf(t, exchangeCode(r, code, ""))
	req := httptest.NewRequest("POST", "/userinfo?x=%zz", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUserinfo_PostIgnoresQueryAccessToken(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	tok := accessTokenOf(t, exchangeCode(r, code, ""))
	req := httptest.NewRequest("POST", "/userinfo?access_token="+url.QueryEscape(tok), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserinfo_GetIgnoresQueryAccessToken(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	tok := accessTokenOf(t, exchangeCode(r, code, ""))
	req := httptest.NewRequest("GET", "/userinfo?access_token="+url.QueryEscape(tok), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserinfo_PostRepeatedFormTokenIsInvalidRequest(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	tok := accessTokenOf(t, exchangeCode(r, code, ""))

	// RFC 6750 3.1: a parameter sent more than once is invalid_request, even
	// when the first value is a valid token.
	w := postUserinfo(r, "", url.Values{"access_token": {tok, "other"}}.Encode())
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.JSONEq(t, `{"error":"invalid_request","error_description":"access_token must be sent only once"}`, w.Body.String())
}
