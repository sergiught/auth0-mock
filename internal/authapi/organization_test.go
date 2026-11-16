package authapi

import (
	"encoding/json"
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
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

const acmeOrgID = "org_822b33ad87c148a0"

func TestResolveOrganization(t *testing.T) {
	acme := organization{ID: acmeOrgID, Name: "acme"}
	tests := []struct {
		in   string
		want organization
	}{
		{"", organization{}},
		{"   ", organization{}},
		{"org_abc123", organization{ID: "org_abc123"}},
		{"acme", acme},
		{"Acme", acme},
		{" acme ", acme},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, resolveOrganization(tc.in), "input %q", tc.in)
	}
}

func TestAddClaims(t *testing.T) {
	m := map[string]any{}
	organization{}.addClaims(m)
	assert.NotContains(t, m, "org_id")
	assert.NotContains(t, m, "org_name")

	m = map[string]any{}
	organization{ID: "org_x"}.addClaims(m)
	assert.Equal(t, "org_x", m["org_id"])
	assert.NotContains(t, m, "org_name")

	m = map[string]any{}
	organization{ID: "org_x", Name: "x"}.addClaims(m)
	assert.Equal(t, "org_x", m["org_id"])
	assert.Equal(t, "x", m["org_name"])
}

func newOrgRouter(t *testing.T) (chi.Router, *jwks.KeySet, *claims.Store) {
	t.Helper()
	return newOrgRouterStrict(t, false)
}

func newOrgRouterStrict(t *testing.T, strict bool) (chi.Router, *jwks.KeySet, *claims.Store) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	store := claims.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(), Sessions: session.NewStore(), Claims: store, TokenEndpointStrict: strict,
	})
	return r, ks, store
}

// orgCodeTokens runs a code flow and returns the verified access and ID token claims.
func orgCodeTokens(t *testing.T, r chi.Router, ks *jwks.KeySet, extra url.Values, tokenExtra url.Values) (access, id *jwks.Claims) {
	t.Helper()
	extra.Set("scope", "openid")
	code := authorizeCode(t, r, extra)
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"demo"}, "code": {code},
		"redirect_uri": {"https://app/cb"}, "audience": {"https://api/"},
	}
	for k, v := range tokenExtra {
		form[k] = v
	}
	w := postTokenForm(r, form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return verified(t, ks, resp.AccessToken), verified(t, ks, resp.IDToken)
}

func TestAuthorize_CodeFlowOrganization(t *testing.T) {
	r, ks, _ := newOrgRouter(t)

	access, id := orgCodeTokens(t, r, ks, url.Values{"organization": {"acme"}}, nil)
	for _, c := range []*jwks.Claims{access, id} {
		assert.Equal(t, acmeOrgID, c.Extra["org_id"])
		assert.Equal(t, "acme", c.Extra["org_name"])
	}

	access, id = orgCodeTokens(t, r, ks, url.Values{"organization": {"org_abc123"}}, nil)
	for _, c := range []*jwks.Claims{access, id} {
		assert.Equal(t, "org_abc123", c.Extra["org_id"])
		assert.NotContains(t, c.Extra, "org_name")
	}

	access, id = orgCodeTokens(t, r, ks, url.Values{"organization": {"acme"}}, url.Values{"organization": {"other"}})
	for _, c := range []*jwks.Claims{access, id} {
		assert.Equal(t, acmeOrgID, c.Extra["org_id"])
		assert.Equal(t, "acme", c.Extra["org_name"])
	}
}

func TestAuthorize_ImplicitHybridOrganization(t *testing.T) {
	r, ks, _ := newOrgRouter(t)
	for _, rt := range []string{"token%20id_token", "code%20id_token"} {
		frag := fragmentOf(t, r, implicitBase+"nonce=n&organization=org_abc123&audience=https%3A%2F%2Fapi%2F&response_type="+rt)
		toks := []string{frag.Get("id_token")}
		if frag.Has("access_token") {
			toks = append(toks, frag.Get("access_token"))
		}
		for _, tok := range toks {
			c := verified(t, ks, tok)
			assert.Equal(t, "org_abc123", c.Extra["org_id"], rt)
			assert.NotContains(t, c.Extra, "org_name", rt)
		}
	}
	// The hybrid code carries it to the exchange too.
	frag := fragmentOf(t, r, implicitBase+"nonce=n&organization=acme&response_type=code%20id_token")
	idc := verified(t, ks, frag.Get("id_token"))
	assert.Equal(t, acmeOrgID, idc.Extra["org_id"])
	assert.Equal(t, "acme", idc.Extra["org_name"])
	w := postTokenForm(r, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"demo"}, "code": {frag.Get("code")},
		"redirect_uri": {"https://app/cb"}, "audience": {"https://api/"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for _, tok := range []string{resp.AccessToken, resp.IDToken} {
		c := verified(t, ks, tok)
		assert.Equal(t, acmeOrgID, c.Extra["org_id"])
		assert.Equal(t, "acme", c.Extra["org_name"])
	}
}

func TestAuthorize_SilentLoginOrganization(t *testing.T) {
	r, ks, _, _ := newSilentRouter(t, session.NewStore())
	cookie := aliceLogin(t, r)
	w := silentAuthorize(r, cookie, url.Values{"prompt": {"none"}, "organization": {"org_abc123"}})
	c := fragmentIDClaims(t, ks, w)
	assert.Equal(t, "org_abc123", c.Extra["org_id"])
}

func TestAuthorize_NoOrganization(t *testing.T) {
	r, ks, _ := newOrgRouter(t)
	access, id := orgCodeTokens(t, r, ks, url.Values{}, nil)
	for _, c := range []*jwks.Claims{access, id} {
		assert.NotContains(t, c.Extra, "org_id")
		assert.NotContains(t, c.Extra, "org_name")
	}
}

func TestAuthorize_GlobalClaimOverridesOrg(t *testing.T) {
	r, ks, store := newOrgRouter(t)
	store.Set(map[string]any{"org_id": "org_global"})
	access, id := orgCodeTokens(t, r, ks, url.Values{"organization": {"org_abc123"}}, nil)
	assert.Equal(t, "org_global", access.Extra["org_id"])
	assert.Equal(t, "org_abc123", id.Extra["org_id"])
}

func TestAuthorize_ClaimMappingOverridesOrg(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	mappings := claims.NewMappingStore()
	mappings.Set(map[string]string{"tenant": "org_id"})
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(), Sessions: session.NewStore(), ClaimMappings: mappings,
	})
	access, id := orgCodeTokens(t, r, ks, url.Values{"organization": {"org_abc123"}}, url.Values{"tenant": {"org_mapped"}})
	assert.Equal(t, "org_mapped", access.Extra["org_id"])
	assert.Equal(t, "org_abc123", id.Extra["org_id"])
}

func orgTokenPost(t *testing.T, r chi.Router, form url.Values) tokenResponse {
	t.Helper()
	w := postTokenForm(r, form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestRefreshKeepsOrganization(t *testing.T) {
	for _, strict := range []bool{false, true} {
		r, ks, _ := newOrgRouterStrict(t, strict)
		code := authorizeCode(t, r, url.Values{"organization": {"acme"}, "scope": {"openid offline_access"}})
		resp := orgTokenPost(t, r, url.Values{
			"grant_type": {"authorization_code"}, "client_id": {"demo"}, "code": {code},
			"redirect_uri": {"https://app/cb"}, "audience": {"https://api/"},
		})
		require.NotEmpty(t, resp.RefreshToken, "strict=%v", strict)
		rt := resp.RefreshToken
		for i := 0; i < 2; i++ {
			resp = orgTokenPost(t, r, url.Values{
				"grant_type": {"refresh_token"}, "client_id": {"demo"}, "refresh_token": {rt},
				"organization": {"other"},
			})
			for _, tok := range []string{resp.AccessToken, resp.IDToken} {
				c := verified(t, ks, tok)
				assert.Equal(t, acmeOrgID, c.Extra["org_id"], "strict=%v i=%d", strict, i)
				assert.Equal(t, "acme", c.Extra["org_name"], "strict=%v i=%d", strict, i)
			}
			if resp.RefreshToken != "" {
				rt = resp.RefreshToken
			}
		}
	}
}

func TestClientCredentialsOrganization(t *testing.T) {
	r, ks, _ := newOrgRouter(t)
	form := url.Values{
		"grant_type": {"client_credentials"}, "client_id": {"demo"}, "client_secret": {"s"},
		"audience": {"https://api/"}, "organization": {"acme"},
	}
	c := verified(t, ks, orgTokenPost(t, r, form).AccessToken)
	assert.Equal(t, "client-credentials", c.Extra["gty"])
	assert.Equal(t, acmeOrgID, c.Extra["org_id"])
	assert.Equal(t, "acme", c.Extra["org_name"])

	form.Del("organization")
	c = verified(t, ks, orgTokenPost(t, r, form).AccessToken)
	assert.NotContains(t, c.Extra, "org_id")
	assert.NotContains(t, c.Extra, "org_name")

	body := `{"grant_type":"client_credentials","client_id":"demo","client_secret":"s","audience":"https://api/","organization":"acme"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	c = verified(t, ks, resp.AccessToken)
	assert.Equal(t, acmeOrgID, c.Extra["org_id"])
	assert.Equal(t, "acme", c.Extra["org_name"])
}

func TestPasswordIgnoresOrganization(t *testing.T) {
	r, ks, _ := newOrgRouter(t)
	resp := orgTokenPost(t, r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice"}, "password": {"x"},
		"scope": {"openid"}, "audience": {"https://api/"}, "organization": {"acme"},
	})
	for _, tok := range []string{resp.AccessToken, resp.IDToken} {
		c := verified(t, ks, tok)
		assert.NotContains(t, c.Extra, "org_id")
		assert.NotContains(t, c.Extra, "org_name")
	}
}
