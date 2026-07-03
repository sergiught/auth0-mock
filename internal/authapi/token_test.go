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
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:               zerolog.Nop(),
		LogoutAllowedURLs: []string{"https://app/bye"},
	})
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

	claims, err := ks.Verify(body.AccessToken, jwks.VerifyOpts{})
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

	idClaims, err := ks.Verify(body.IDToken, jwks.VerifyOpts{})
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
		"role":        "admin", // Brand-new claim, also takes.
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
	c, err := ks.Verify(body.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)

	assert.Equal(t, "OVERRIDDEN", c.Extra["gty"], "custom gty must beat the handler-set client-credentials")
	assert.Equal(t, "OVERRIDDEN", c.Extra["azp"], "custom azp must beat the handler-set client_id")
	assert.Equal(t, []any{"custom:scope"}, c.Extra["permissions"],
		"custom permissions must beat the /admin0/permissions injection")
	assert.Equal(t, "admin", c.Extra["role"], "brand-new claims pass through")
}

// newAuthRouterWithMappings wires a router with a claim-mapping store (and a
// claims store, so precedence between the two is testable).
func newAuthRouterWithMappings(t *testing.T) (chi.Router, *jwks.KeySet, *claims.MappingStore, *claims.Store) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	mappings := claims.NewMappingStore()
	claimsStore := claims.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:           zerolog.Nop(),
		Claims:        claimsStore,
		ClaimMappings: mappings,
	})
	return r, ks, mappings, claimsStore
}

// mintClientCredentials posts a client_credentials form request with the
// given extra params and returns the verified access-token claims.
func mintClientCredentials(t *testing.T, r chi.Router, ks *jwks.KeySet, extra url.Values) *jwks.Claims {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "abc")
	form.Set("client_secret", "x")
	form.Set("audience", "https://api/")
	maps.Copy(form, extra)
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	c, err := ks.Verify(body.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	return c
}

// TestToken_ClaimMappings_ProjectsRequestParam covers the core acceptance
// criteria of the claim-mapping feature: with a mapping registered, the
// mapped request parameter lands in the minted token under the claim name —
// per request, without touching /admin0/claims — and two sequential requests
// with different values mint tokens with the respective values.
func TestToken_ClaimMappings_ProjectsRequestParam(t *testing.T) {
	r, ks, mappings, _ := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})

	c1 := mintClientCredentials(t, r, ks, url.Values{"resource": []string{"urn:api:orders"}})
	assert.Equal(t, "urn:api:orders", c1.Extra["https://example.com/resource"])

	// Second mint with a different value — no global-state race.
	c2 := mintClientCredentials(t, r, ks, url.Values{"resource": []string{"urn:api:billing"}})
	assert.Equal(t, "urn:api:billing", c2.Extra["https://example.com/resource"])

	// Param omitted → claim absent, exactly as today.
	c3 := mintClientCredentials(t, r, ks, nil)
	assert.NotContains(t, c3.Extra, "https://example.com/resource")

	// Params outside the allowlist never reach the token.
	c4 := mintClientCredentials(t, r, ks, url.Values{"rogue": []string{"nope"}})
	assert.NotContains(t, c4.Extra, "rogue")
}

// TestToken_ClaimMappings_JSONBody_PrivateKeyJWT covers the private_key_jwt
// client-credentials variant: a JSON body carrying client_assertion instead
// of client_secret. The mock doesn't validate client auth, so the variant
// reduces to "params must be captured from JSON bodies too".
func TestToken_ClaimMappings_JSONBody_PrivateKeyJWT(t *testing.T) {
	r, ks, mappings, _ := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})

	body := `{
		"grant_type": "client_credentials",
		"client_id": "abc",
		"client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		"client_assertion": "eyJ.fake.jwt",
		"audience": "https://api/",
		"resource": "urn:api:orders"
	}`
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	c, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "urn:api:orders", c.Extra["https://example.com/resource"])
}

// TestToken_ClaimMappings_OverrideGlobalClaims nails down precedence: a
// per-request value beats the global claims store for the same claim, and
// the global value still applies when the parameter is omitted.
func TestToken_ClaimMappings_OverrideGlobalClaims(t *testing.T) {
	r, ks, mappings, claimsStore := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})
	claimsStore.Set(map[string]any{"https://example.com/resource": "global-default"})

	c := mintClientCredentials(t, r, ks, url.Values{"resource": []string{"urn:api:orders"}})
	assert.Equal(t, "urn:api:orders", c.Extra["https://example.com/resource"],
		"per-request parameter must beat the global claims store")

	c = mintClientCredentials(t, r, ks, nil)
	assert.Equal(t, "global-default", c.Extra["https://example.com/resource"],
		"global claims still apply when the parameter is omitted")
}

// TestToken_ClaimMappings_RepeatedFormParam_FirstWins pins the duplicate-
// parameter edge for form bodies: `resource=a&resource=b` projects the
// FIRST value (parseTokenRequest takes PostForm's vs[0], matching
// url.Values.Get semantics used for every other token-request field).
func TestToken_ClaimMappings_RepeatedFormParam_FirstWins(t *testing.T) {
	r, ks, mappings, _ := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})

	c := mintClientCredentials(t, r, ks, url.Values{
		"resource": []string{"urn:api:first", "urn:api:second"},
	})
	assert.Equal(t, "urn:api:first", c.Extra["https://example.com/resource"])
}

// TestToken_ClaimMappings_CanTargetReservedClaims pins that projection is
// the final layer: a mapping targeting a reserved claim (gty here) beats
// the grant handler's value, mirroring the documented claims-store
// philosophy that tests can override anything.
func TestToken_ClaimMappings_CanTargetReservedClaims(t *testing.T) {
	r, ks, mappings, _ := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"custom_gty": "gty"})

	c := mintClientCredentials(t, r, ks, url.Values{"custom_gty": []string{"totally-custom"}})
	assert.Equal(t, "totally-custom", c.Extra["gty"])
}

// TestToken_ClaimMappings_EmptyFormParam_TreatedAsOmitted pins RFC 6749
// §3.2: a form parameter sent without a value must behave as if omitted —
// it must not project, and must not clobber a seeded global default.
func TestToken_ClaimMappings_EmptyFormParam_TreatedAsOmitted(t *testing.T) {
	r, ks, mappings, claimsStore := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})
	claimsStore.Set(map[string]any{"https://example.com/resource": "global-default"})

	c := mintClientCredentials(t, r, ks, url.Values{"resource": []string{""}})
	assert.Equal(t, "global-default", c.Extra["https://example.com/resource"],
		"empty-valued form parameter must be treated as omitted (RFC 6749 §3.2)")
}

// TestToken_ClaimMappings_DuplicateJSONKey_LastWins pins the documented
// JSON-body twin of the repeated-form-param rule: a duplicate key in a
// JSON token request keeps the last value (standard encoding/json map
// decoding). Guards the OpenAPI description against a stricter-decoder
// refactor.
func TestToken_ClaimMappings_DuplicateJSONKey_LastWins(t *testing.T) {
	r, ks, mappings, _ := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})

	body := `{"grant_type":"client_credentials","client_id":"abc","client_secret":"x",` +
		`"audience":"https://api/","resource":"urn:api:first","resource":"urn:api:last"}`
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	c, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "urn:api:last", c.Extra["https://example.com/resource"])
}

// TestToken_ClaimMappings_MFAGrant_ProjectsParams pins the params
// threading through mintFromMFA: the three mfa-* grants cross a file and
// a signature change to reach augmentExtra, so a refactor passing nil
// would compile and pass every other test while silently disabling
// projection for step-up tokens.
func TestToken_ClaimMappings_MFAGrant_ProjectsParams(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	mappings := claims.NewMappingStore()
	mappings.Set(map[string]string{"resource": "https://example.com/resource"})
	mfaStore := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:           zerolog.Nop(),
		ClaimMappings: mappings,
		MFA:           mfaStore,
	})
	tok := mfaStore.Issue(mfa.Context{
		ClientID: "abc", Audience: "https://api/", Scope: "openid", Subject: "alice",
	})

	form := url.Values{
		"grant_type": []string{"http://auth0.com/oauth/grant-type/mfa-otp"},
		"client_id":  []string{"abc"},
		"mfa_token":  []string{tok},
		"otp":        []string{mfa.AcceptedOTP},
		"resource":   []string{"urn:api:orders"},
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	c, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "mfa-otp", c.Extra["gty"])
	assert.Equal(t, "urn:api:orders", c.Extra["https://example.com/resource"])
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
	c, err := ks.Verify(body.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "password-realm", c.Extra["gty"])
	assert.Equal(t, "Username-Password-Authentication", c.Extra["connection"])
	assert.Equal(t, "Username-Password-Authentication", c.Extra["https://auth0.com/realm"])
	assert.Equal(t, "native-app", c.Extra["azp"])
}

// TestToken_PasswordRealm_MFA_RequiredReturnsMFAToken nails down that the
// realm-aware password grant goes through the same MFA-required gate as
// the plain password grant. The native SDKs (auth0-android, auth0-swift,
// auth0-react-native) use password-realm, so a regression here would
// silently bypass MFA on every native client.
func TestToken_PasswordRealm_MFA_RequiredReturnsMFAToken(t *testing.T) {
	r, _, mfaStore := newAuthRouterWithMFA(t)
	mfaStore.SetRequired(true)

	form := url.Values{}
	form.Set("grant_type", "http://auth0.com/oauth/grant-type/password-realm")
	form.Set("client_id", "native-app")
	form.Set("username", "alice@example.com")
	form.Set("password", "ignored")
	form.Set("realm", "Username-Password-Authentication")
	form.Set("audience", "https://api/")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "mfa_required", body["error"])
	assert.NotEmpty(t, body["mfa_token"])
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
				"grant_type":   []string{tc.grantType},
				"client_id":    []string{"abc"},
				"mfa_token":    []string{tok},
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
			c, err := ks.Verify(body.AccessToken, jwks.VerifyOpts{})
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
		// Deliberately no oob_code.
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
		// Deliberately no mfa_token.
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "mfa_token")
}
