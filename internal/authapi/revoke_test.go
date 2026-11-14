package authapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func postRevoke(r chi.Router, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRevoke_Strict_RevokesChain(t *testing.T) {
	r, rt1 := loginForRefresh(t, true)
	rt2 := refreshOK(t, r, rt1, "demo")

	w := postRevoke(r, "application/x-www-form-urlencoded", url.Values{"token": {rt1}}.Encode())
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())

	requireUnknownRefresh(t, refreshReq(r, rt2, "demo"))
}

func TestRevoke_Strict_JSONBody(t *testing.T) {
	r, rt1 := loginForRefresh(t, true)

	w := postRevoke(r, "application/json", `{"token":"`+rt1+`","client_id":"demo"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())

	requireUnknownRefresh(t, refreshReq(r, rt1, "demo"))
}

func TestRevoke_Strict_UnknownToken200(t *testing.T) {
	r, _ := loginForRefresh(t, true)
	w := postRevoke(r, "application/x-www-form-urlencoded", "token=nope")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestRevoke_Lenient_NoOp(t *testing.T) {
	r, rt1 := loginForRefresh(t, false)
	w := postRevoke(r, "application/x-www-form-urlencoded", url.Values{"token": {rt1}}.Encode())
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())

	assert.Equal(t, http.StatusOK, refreshReq(r, rt1, "demo").Code)
}
