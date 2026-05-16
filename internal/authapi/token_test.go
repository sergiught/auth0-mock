package authapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
)

func newAuthRouter(t *testing.T) (chi.Router, *jwks.KeySet) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	r := chi.NewRouter()
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

// TestToken_CustomClaims_OverrideReserved nails down the documented design:
// /admin0/claims entries win over the reserved claims the grant handler sets
// (gty, azp), and over the permissions claim injected from
// /admin0/permissions. Adopters who lean on this for tests would notice
// instantly if it regressed — there's no other test that catches it.
func TestToken_CustomClaims_OverrideReserved(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	claimsStore := claims.NewStore()
	claimsStore.Set(map[string]any{
		"gty":         "OVERRIDDEN",
		"azp":         "OVERRIDDEN",
		"permissions": []any{"custom:scope"},
		"role":        "admin", // brand-new claim, also takes
	})

	permsStore := permissions.NewStore()
	permsStore.Set("https://api/", []string{"would-be-overridden"})

	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:         zerolog.Nop(),
		Claims:      claimsStore,
		Permissions: permsStore,
	})

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "real-client")
	form.Set("client_secret", "x")
	form.Set("audience", "https://api/")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	c, err := ks.Verify(body.AccessToken)
	require.NoError(t, err)

	assert.Equal(t, "OVERRIDDEN", c.Extra["gty"], "custom gty must beat the handler-set client-credentials")
	assert.Equal(t, "OVERRIDDEN", c.Extra["azp"], "custom azp must beat the handler-set client_id")
	assert.Equal(t, []any{"custom:scope"}, c.Extra["permissions"],
		"custom permissions must beat the /admin0/permissions injection")
	assert.Equal(t, "admin", c.Extra["role"], "brand-new claims pass through")
}

// newAuthRouterWithMFA wires a router that has an MFA store attached, so
// the password / password-realm grants can issue mfa_tokens and the
// mfa-* grants have a Consume target.
func newAuthRouterWithMFA(t *testing.T) (chi.Router, *jwks.KeySet, *mfa.Store) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	mfaStore := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(),
		MFA: mfaStore,
	})
	return r, ks, mfaStore
}

// TestToken_PasswordRealm_MissingRealm_400 covers respondPasswordRealm's
// guard clause. The grant_type is Auth0-specific and used by the native
// SDKs; an SDK that forgets to thread the realm through must fail loudly.
func TestToken_PasswordRealm_MissingRealm_400(t *testing.T) {
	r, _ := newAuthRouter(t)
	form := url.Values{}
	form.Set("grant_type", "http://auth0.com/oauth/grant-type/password-realm")
	form.Set("client_id", "abc")
	form.Set("username", "alice@example.com")
	form.Set("password", "ignored")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"invalid_request"`)
	assert.Contains(t, w.Body.String(), "realm")
}

// TestToken_PasswordRealm_MintsTokenWithConnectionClaim covers the happy
// path. The minted access token must carry the realm in three places:
// connection, https://auth0.com/realm, and gty=password-realm — matching
// what real Auth0 emits and what the Auth0 Android / Swift / RN SDKs
// look for.
func TestToken_PasswordRealm_MintsTokenWithConnectionClaim(t *testing.T) {
	r, ks := newAuthRouter(t)
	form := url.Values{}
	form.Set("grant_type", "http://auth0.com/oauth/grant-type/password-realm")
	form.Set("client_id", "native-app")
	form.Set("username", "alice@example.com")
	form.Set("password", "ignored")
	form.Set("realm", "Username-Password-Authentication")
	form.Set("audience", "https://api.example.com/")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	c, err := ks.Verify(body.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "password-realm", c.Extra["gty"])
	assert.Equal(t, "Username-Password-Authentication", c.Extra["connection"])
	assert.Equal(t, "Username-Password-Authentication", c.Extra["https://auth0.com/realm"])
	assert.Equal(t, "native-app", c.Extra["azp"])
}

// TestToken_MFA_RequiredReturnsMFAToken covers the first half of the MFA
// dance: with enforcement on, a password grant must NOT mint a token but
// must return 403 + an mfa_token the client can exchange in step 2.
func TestToken_MFA_RequiredReturnsMFAToken(t *testing.T) {
	r, _, mfaStore := newAuthRouterWithMFA(t)
	mfaStore.SetRequired(true)

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", "abc")
	form.Set("username", "alice@example.com")
	form.Set("password", "ignored")
	form.Set("audience", "https://api/")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "mfa_required", body["error"])
	assert.NotEmpty(t, body["mfa_token"], "client needs the mfa_token to exchange in step 2")
}

// mfaGrantTable drives the three step-2 MFA grants through a single shape.
// Each row covers: happy path + at least one rejection (wrong factor /
// missing factor / wrong mfa_token).
var mfaGrantTable = []struct {
	name        string
	grantType   string
	factorField string
	correct     string
	wrong       string
	gtyClaim    string
	extraForm   url.Values
}{
	{
		name:        "mfa-otp",
		grantType:   "http://auth0.com/oauth/grant-type/mfa-otp",
		factorField: "otp",
		correct:     mfa.AcceptedOTP,
		wrong:       "000000",
		gtyClaim:    "mfa-otp",
	},
	{
		name:        "mfa-oob",
		grantType:   "http://auth0.com/oauth/grant-type/mfa-oob",
		factorField: "binding_code",
		correct:     mfa.AcceptedBindingCode,
		wrong:       "000000",
		gtyClaim:    "mfa-oob",
		extraForm:   url.Values{"oob_code": []string{"push-abc"}},
	},
	{
		name:        "mfa-recovery-code",
		grantType:   "http://auth0.com/oauth/grant-type/mfa-recovery-code",
		factorField: "recovery_code",
		correct:     mfa.AcceptedRecoveryCode,
		wrong:       "WRONG-RECOVERY-CD",
		gtyClaim:    "mfa-recovery-code",
	},
}

func TestToken_MFAGrants_HappyAndUnhappy(t *testing.T) {
	for _, tc := range mfaGrantTable {
		t.Run(tc.name, func(t *testing.T) {
			r, ks, mfaStore := newAuthRouterWithMFA(t)
			// Pre-issue an mfa_token as if the client had just done step 1.
			tok := mfaStore.Issue(mfa.Context{
				ClientID: "abc",
				Audience: "https://api/",
				Scope:    "openid",
				Subject:  "alice@example.com",
			})

			// Happy path: correct factor.
			form := url.Values{
				"grant_type": []string{tc.grantType},
				"client_id":  []string{"abc"},
				"mfa_token":  []string{tok},
				tc.factorField: []string{tc.correct},
			}
			maps.Copy(form, tc.extraForm)
			req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var body tokenResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			c, err := ks.Verify(body.AccessToken)
			require.NoError(t, err)
			assert.Equal(t, tc.gtyClaim, c.Extra["gty"], "minted token must carry the step-up grant type")

			// Re-issue, then exchange with the wrong factor → 403.
			tok = mfaStore.Issue(mfa.Context{
				ClientID: "abc", Audience: "https://api/", Subject: "alice@example.com",
			})
			form.Set("mfa_token", tok)
			form.Set(tc.factorField, tc.wrong)
			req = httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w = httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"invalid_grant"`)

			// Unknown mfa_token (e.g. expired or never issued) → 403.
			form.Set("mfa_token", "never-existed")
			form.Set(tc.factorField, tc.correct)
			req = httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w = httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"invalid_grant"`)
		})
	}
}

// TestToken_MFAOOB_MissingOOBCode_400 covers the OOB-specific guard: the
// oob_code field is required even though binding_code is what's actually
// verified. The order here matters — the handler consumes the mfa_token
// before checking oob_code, so a missing oob_code burns the token. That's
// the documented behaviour.
func TestToken_MFAOOB_MissingOOBCode_400(t *testing.T) {
	r, _, mfaStore := newAuthRouterWithMFA(t)
	tok := mfaStore.Issue(mfa.Context{
		ClientID: "abc", Audience: "https://api/", Subject: "alice",
	})
	form := url.Values{
		"grant_type":   []string{"http://auth0.com/oauth/grant-type/mfa-oob"},
		"client_id":    []string{"abc"},
		"mfa_token":    []string{tok},
		"binding_code": []string{mfa.AcceptedBindingCode},
		// deliberately no oob_code
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "oob_code")
}

// TestToken_MFA_MissingMFAToken_400 covers consumeMFAToken's empty-token
// guard. Triggers on any of the three MFA grants; tested on mfa-otp here.
func TestToken_MFA_MissingMFAToken_400(t *testing.T) {
	r, _, _ := newAuthRouterWithMFA(t)
	form := url.Values{
		"grant_type": []string{"http://auth0.com/oauth/grant-type/mfa-otp"},
		"client_id":  []string{"abc"},
		"otp":        []string{mfa.AcceptedOTP},
		// deliberately no mfa_token
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "mfa_token")
}
