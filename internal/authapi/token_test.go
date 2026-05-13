package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
)

func newAuthRouter(t *testing.T) (*httprouter.Router, *jwks.KeySet) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	r := httprouter.New()
	Mount(Deps{Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/", Log: zerolog.Nop()})
	return r, ks
}

func TestToken_ClientCredentials_Form(t *testing.T) {
	r, ks := newAuthRouter(t)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "abc")
	form.Set("client_secret", "xyz")
	form.Set("audience", "https://api/")
	form.Set("scope", "read:users")

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body.AccessToken)
	assert.Equal(t, "Bearer", body.TokenType)
	assert.Equal(t, "read:users", body.Scope)
	assert.Empty(t, body.IDToken, "client_credentials must NOT issue id_token")

	claims, err := ks.Verify(body.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "abc@clients", claims.Subject)
	assert.Contains(t, claims.Audience, "https://api/")
	assert.Equal(t, "client-credentials", claims.Extra["gty"])
	assert.Equal(t, "abc", claims.Extra["azp"])
}

func TestToken_ClientCredentials_JSONBody(t *testing.T) {
	r, _ := newAuthRouter(t)
	body := `{"grant_type":"client_credentials","client_id":"abc","client_secret":"x","audience":"https://api/"}`
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestToken_MissingGrantType_400(t *testing.T) {
	r, _ := newAuthRouter(t)
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"invalid_request"`)
}

func TestToken_UnknownGrantType_400(t *testing.T) {
	r, _ := newAuthRouter(t)
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(`{"grant_type":"weird"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"unsupported_grant_type"`)
}

func TestToken_Password_IncludesIDToken(t *testing.T) {
	r, ks := newAuthRouter(t)

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", "alice@example.com")
	form.Set("password", "ignored")
	form.Set("client_id", "abc")
	form.Set("audience", "https://api/")
	form.Set("scope", "openid profile email")

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body.AccessToken)
	assert.NotEmpty(t, body.IDToken)
	assert.NotEmpty(t, body.RefreshToken)

	idClaims, err := ks.Verify(body.IDToken)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", idClaims.Extra["email"])
}

func TestToken_RefreshToken_MintsNewAccessToken(t *testing.T) {
	r, _ := newAuthRouter(t)

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "any-uuid")
	form.Set("client_id", "abc")

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body.AccessToken)
}

func TestToken_AuthorizationCode_IncludesIDToken(t *testing.T) {
	r, _ := newAuthRouter(t)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "any-code")
	form.Set("client_id", "abc")
	form.Set("redirect_uri", "https://app/callback")

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body.AccessToken)
	assert.NotEmpty(t, body.IDToken)
}
