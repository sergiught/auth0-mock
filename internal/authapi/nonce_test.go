package authapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/pkce"
)

const nonceTestVerifier = "the-quick-brown-fox-jumps-over-the-lazy-dog-43"

// steppedClock is a mutable time source shared by the keyset and the PKCE
// store so a test can make /authorize and /oauth/token happen at distinct
// instants.
type steppedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *steppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *steppedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newNonceRouter(t *testing.T) (chi.Router, *jwks.KeySet, *steppedClock) {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:  zerolog.Nop(),
		PKCE: pkce.NewStore(pkce.WithNow(clk.Now)),
	})
	return r, ks, clk
}

// authorizeCode hits /authorize and returns the issued code.
func authorizeCode(t *testing.T, r chi.Router, extra url.Values) string {
	t.Helper()
	q := url.Values{
		"client_id":     {"demo"},
		"redirect_uri":  {"https://app/cb"},
		"response_type": {"code"},
	}
	for k, v := range extra {
		q[k] = v
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

func postTokenForm(r chi.Router, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func exchangeCode(r chi.Router, code, verifier string) *httptest.ResponseRecorder {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {"demo"},
		"code":         {code},
		"redirect_uri": {"https://app/cb"},
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return postTokenForm(r, form)
}

func idTokenClaims(t *testing.T, ks *jwks.KeySet, w *httptest.ResponseRecorder) *jwks.Claims {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.IDToken)
	c, err := ks.Verify(resp.IDToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	return c
}

func TestAuthorizationCode_EchoesNonceWithoutPKCE(t *testing.T) {
	r, ks, clk := newNonceRouter(t)
	authTime := clk.Now()

	code := authorizeCode(t, r, url.Values{"nonce": {"n-123"}})
	clk.Advance(30 * time.Second)

	c := idTokenClaims(t, ks, exchangeCode(r, code, ""))
	assert.Equal(t, "n-123", c.Extra["nonce"])
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.EqualValues(t, authTime.Unix(), c.Extra["auth_time"])
	assert.Contains(t, c.Extra, "email")
}

func TestAuthorizationCode_EchoesNonceWithPKCE_StillEnforced(t *testing.T) {
	r, ks, _ := newNonceRouter(t)
	challenge := pkceS256(nonceTestVerifier)

	extra := url.Values{
		"nonce": {"n-pkce"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	code := authorizeCode(t, r, extra)
	c := idTokenClaims(t, ks, exchangeCode(r, code, nonceTestVerifier))
	assert.Equal(t, "n-pkce", c.Extra["nonce"])

	code = authorizeCode(t, r, extra)
	w := exchangeCode(r, code, "wrong-verifier-wrong-verifier-wrong-verifier-xx")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	code = authorizeCode(t, r, extra)
	w = exchangeCode(r, code, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthorizationCode_NoNonceOmitsClaim(t *testing.T) {
	r, ks, clk := newNonceRouter(t)
	code := authorizeCode(t, r, nil)
	c := idTokenClaims(t, ks, exchangeCode(r, code, ""))
	assert.NotContains(t, c.Extra, "nonce")
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.EqualValues(t, clk.Now().Unix(), c.Extra["auth_time"])
}

func TestAuthorizationCode_UnknownCodeIsLenient(t *testing.T) {
	r, ks, clk := newNonceRouter(t)
	c := idTokenClaims(t, ks, exchangeCode(r, "never-issued", ""))
	assert.NotContains(t, c.Extra, "nonce")
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.EqualValues(t, clk.Now().Unix(), c.Extra["auth_time"])
}

func TestAuthorizationCode_NonPKCEEntryRejectsVerifier(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"nonce": {"n-1"}})
	w := exchangeCode(r, code, nonceTestVerifier)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_grant")
	assert.Contains(t, w.Body.String(), "no stored challenge")
}

func TestPasswordGrants_IDTokenCarriesAuthTimeAndAzp(t *testing.T) {
	r, ks, clk := newNonceRouter(t)

	w := postTokenForm(r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice"}, "password": {"x"},
	})
	c := idTokenClaims(t, ks, w)
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.EqualValues(t, clk.Now().Unix(), c.Extra["auth_time"])

	w = postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/password-realm"}, "client_id": {"demo"},
		"username": {"alice"}, "password": {"x"}, "realm": {"db"},
	})
	c = idTokenClaims(t, ks, w)
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.EqualValues(t, clk.Now().Unix(), c.Extra["auth_time"])
}

func TestMFAGrant_IDTokenCarriesAuthTimeAndAzp(t *testing.T) {
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	mfaStore := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), MFA: mfaStore,
	})
	tok := mfaStore.Issue(mfa.Context{
		ClientID: "abc", Audience: "https://api/", Scope: "openid", Subject: "alice",
	})
	w := postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/mfa-otp"}, "client_id": {"abc"},
		"mfa_token": {tok}, "otp": {mfa.AcceptedOTP},
	})
	c := idTokenClaims(t, ks, w)
	assert.Equal(t, "abc", c.Extra["azp"])
	assert.EqualValues(t, clk.Now().Unix(), c.Extra["auth_time"])
	assert.Equal(t, "alice", c.Extra["email"])
	assert.Equal(t, "alice", c.Extra["name"])
	assert.Equal(t, "alice", c.Extra["nickname"])
	assert.Equal(t, "https://cdn.auth0.com/avatars/al.png", c.Extra["picture"])
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestAuthorizationCode_LoginHintBecomesSubjectAndProfile(t *testing.T) {
	r, ks, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	c := idTokenClaims(t, ks, exchangeCode(r, code, ""))
	assert.Equal(t, "alice@example.com", c.Subject)
	assert.Equal(t, "alice", c.Extra["nickname"])
	assert.Equal(t, "alice@example.com", c.Extra["email"])
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.NotNil(t, c.Extra["auth_time"])
}

func TestAuthorizationCode_DefaultSubjectWithoutLoginHint(t *testing.T) {
	r, ks, _ := newNonceRouter(t)
	code := authorizeCode(t, r, nil)
	c := idTokenClaims(t, ks, exchangeCode(r, code, ""))
	assert.Equal(t, DefaultSubject, c.Subject)
	assert.Equal(t, "mock-user@example.com", c.Extra["email"])

	c = idTokenClaims(t, ks, exchangeCode(r, "unknown-code", ""))
	assert.Equal(t, DefaultSubject, c.Subject)
}

func TestPasswordGrants_IDTokenCarriesDerivedProfile(t *testing.T) {
	r, ks, _ := newNonceRouter(t)
	w := postTokenForm(r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"}, "password": {"x"},
	})
	c := idTokenClaims(t, ks, w)
	assert.Equal(t, "alice", c.Extra["nickname"])
	assert.Equal(t, "https://cdn.auth0.com/avatars/al.png", c.Extra["picture"])

	w = postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/password-realm"}, "client_id": {"demo"},
		"username": {"alice@example.com"}, "password": {"x"}, "realm": {"db"},
	})
	c = idTokenClaims(t, ks, w)
	assert.Equal(t, "alice", c.Extra["nickname"])
	assert.Equal(t, "alice@example.com", c.Extra["email"])
}
