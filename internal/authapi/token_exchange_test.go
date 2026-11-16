package authapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
)

const orgAcmeID = "org_822b33ad87c148a0"

func fakeJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	enc := base64.RawURLEncoding
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return enc.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + enc.EncodeToString(raw) + ".sig"
}

func exchangeForm(subject string, extra url.Values) url.Values {
	f := url.Values{
		"grant_type":         {grantTokenExchange},
		"subject_token":      {subject},
		"subject_token_type": {"urn:acme:legacy"},
		"audience":           {"https://api/"},
		"scope":              {"openid"},
		"client_id":          {"app"},
	}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func exchangeTokens(t *testing.T, ks *jwks.KeySet, body []byte) (access, id *jwks.Claims, resp tokenResponse) {
	t.Helper()
	require.NoError(t, json.Unmarshal(body, &resp))
	return verified(t, ks, resp.AccessToken), verified(t, ks, resp.IDToken), resp
}

func TestValidSubjectTokenType(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/legacy":           true,
		"urn:acme:legacy":                      true,
		"http://example.com/x":                 false,
		"legacy":                               false,
		"":                                     false,
		"https://auth0.com/x":                  false,
		"HTTPS://Auth0.com/x":                  false,
		"http://okta.com/x":                    false,
		"urn:ietf:params:oauth:token-type:jwt": false,
		"URN:IETF:x":                           false,
		"urn:auth0:x":                          false,
		"https://auth0.com:443/x":              false,
		"https://auth0.company.com/x":          false,
		"urn:ietfx:foo":                        false,
		"https://sub.auth0.com/x":              true,
		"urn:":                                 true,
		"urn:okta:x":                           false,
	}
	for in, want := range cases {
		assert.Equal(t, want, validSubjectTokenType(in), in)
	}
}

func TestSubjectFromToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sub, profile, ok := subjectFromToken("user-123", now)
	assert.True(t, ok)
	assert.Equal(t, "user-123", sub)
	assert.Empty(t, profile)

	sub, _, ok = subjectFromToken("user.name@example.com", now)
	assert.True(t, ok)
	assert.Equal(t, "user.name@example.com", sub)

	sub, profile, ok = subjectFromToken(fakeJWT(t, map[string]any{
		"sub": "auth0|abc", "email": "a@b.c", "name": "A", "org_id": "org_x", "foo": "bar",
	}), now)
	assert.True(t, ok)
	assert.Equal(t, "auth0|abc", sub)
	assert.Equal(t, "a@b.c", profile["email"])
	assert.Equal(t, "A", profile["name"])
	assert.NotContains(t, profile, "org_id")
	assert.NotContains(t, profile, "foo")

	_, _, ok = subjectFromToken(fakeJWT(t, map[string]any{"email": "a@b.c"}), now)
	assert.False(t, ok)
	_, _, ok = subjectFromToken(fakeJWT(t, map[string]any{"sub": "u", "exp": now.Unix() - 60}), now)
	assert.False(t, ok)
	_, _, ok = subjectFromToken(fakeJWT(t, map[string]any{"sub": "u", "exp": now.Unix() + 60}), now)
	assert.True(t, ok)
	enc := base64.URLEncoding
	padded := enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString([]byte(`{"sub":"pad"}`)) + ".sig"
	require.Contains(t, padded, "=")
	sub, _, ok = subjectFromToken(padded, now)
	assert.True(t, ok)
	assert.Equal(t, "pad", sub)
	// The header is not a JSON object, so this is a literal user id, not a JWT.
	sub, _, ok = subjectFromToken("hdr.payload.", now)
	assert.True(t, ok)
	assert.Equal(t, "hdr.payload.", sub)
	// An unsecured JWT (alg none) has an empty signature and is still a JWT.
	sub, _, ok = subjectFromToken("eyJhbGciOiJub25lIn0.eyJzdWIiOiJhdXRoMHxqYW5lIn0.", now)
	assert.True(t, ok)
	assert.Equal(t, "auth0|jane", sub)
	_, _, ok = subjectFromToken("eyJhbGciOiJub25lIn0.!!!.sig", now)
	assert.False(t, ok)
}

func TestTokenExchange_LiteralSubject(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := postTokenForm(r, exchangeForm("user-123", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	access, id, resp := exchangeTokens(t, ks, w.Body.Bytes())
	assert.Equal(t, "urn:ietf:params:oauth:token-type:access_token", resp.IssuedTokenType)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, "user-123", access.Subject)
	assert.Equal(t, "token-exchange", access.Extra["gty"])
	assert.Equal(t, "app", access.Extra["azp"])
	assert.Equal(t, "user-123", id.Subject)
	assert.Equal(t, "user-123@example.com", id.Extra["email"])
	assert.Contains(t, id.Extra, "nickname")
	assert.Contains(t, id.Extra, "picture")
}

func TestTokenExchange_JWTSubject(t *testing.T) {
	r, ks := newAuthRouter(t)
	jwt := fakeJWT(t, map[string]any{"sub": "auth0|abc", "email": "jane@example.com", "name": "Jane"})
	w := postTokenForm(r, exchangeForm(jwt, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, id, _ := exchangeTokens(t, ks, w.Body.Bytes())
	assert.Equal(t, "auth0|abc", id.Subject)
	assert.Equal(t, "jane@example.com", id.Extra["email"])
	assert.Equal(t, "Jane", id.Extra["name"])
}

func TestTokenExchange_UnsecuredJWTSubject(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := postTokenForm(r, exchangeForm("eyJhbGciOiJub25lIn0.eyJzdWIiOiJhdXRoMHxqYW5lIn0.", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	access, _, _ := exchangeTokens(t, ks, w.Body.Bytes())
	assert.Equal(t, "auth0|jane", access.Subject)
}

func TestTokenExchange_Errors(t *testing.T) {
	r, _ := newAuthRouter(t)
	expired := fakeJWT(t, map[string]any{"sub": "u", "exp": time.Now().Add(-time.Hour).Unix()})
	noSub := fakeJWT(t, map[string]any{"email": "a@b.c"})
	typeErr := "subject_token_type must be a https:// or urn: URI outside the reserved auth0, okta and ietf namespaces"
	cases := map[string]struct {
		form url.Values
		desc string
	}{
		"no subject_token": {func() url.Values { f := exchangeForm("x", nil); f.Del("subject_token"); return f }(),
			"Missing required parameter: subject_token"},
		"no subject_token_type": {func() url.Values { f := exchangeForm("x", nil); f.Del("subject_token_type"); return f }(),
			"Missing required parameter: subject_token_type"},
		"reserved type":   {exchangeForm("x", url.Values{"subject_token_type": {"https://auth0.com/x"}}), typeErr},
		"expired jwt":     {exchangeForm(expired, nil), "Invalid subject token"},
		"jwt without sub": {exchangeForm(noSub, nil), "Invalid subject token"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := postTokenForm(r, tc.form)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "invalid_request", body["error"])
			assert.Equal(t, tc.desc, body["error_description"])
		})
	}
}

func TestTokenExchange_Organization(t *testing.T) {
	r, ks := newAuthRouter(t)
	jwt := fakeJWT(t, map[string]any{"sub": "u1", "org_id": "org_evil"})
	w := postTokenForm(r, exchangeForm(jwt, url.Values{"organization": {"acme"}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	access, id, _ := exchangeTokens(t, ks, w.Body.Bytes())
	for _, c := range []*jwks.Claims{access, id} {
		assert.Equal(t, orgAcmeID, c.Extra["org_id"])
		assert.Equal(t, "acme", c.Extra["org_name"])
	}

	w = postTokenForm(r, exchangeForm(jwt, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	access, id, _ = exchangeTokens(t, ks, w.Body.Bytes())
	for _, c := range []*jwks.Claims{access, id} {
		assert.NotContains(t, c.Extra, "org_id")
		assert.NotContains(t, c.Extra, "org_name")
	}
}

func TestTokenExchange_JSONBody(t *testing.T) {
	r, ks := newAuthRouter(t)
	body := `{"grant_type":"` + grantTokenExchange + `","subject_token":"user-123","subject_token_type":"urn:acme:legacy","audience":"https://api/","client_id":"app"}`
	c := tokenClaims(t, ks, postToken(r, "application/json", body, ""))
	assert.Equal(t, "token-exchange", c.Extra["gty"])
	assert.Equal(t, "user-123", c.Subject)
}

func TestTokenExchange_RefreshKeepsOrg(t *testing.T) {
	r, ks, _ := newOrgRouterStrict(t, true)
	w := postTokenForm(r, exchangeForm("user-123", url.Values{"scope": {"openid offline_access"}, "organization": {"acme"}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.RefreshToken)

	w = postTokenForm(r, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {"app"}, "refresh_token": {resp.RefreshToken},
	})
	c := tokenClaims(t, ks, w)
	assert.Equal(t, orgAcmeID, c.Extra["org_id"])
	_, refreshedID, _ := exchangeTokens(t, ks, w.Body.Bytes())
	assert.Equal(t, orgAcmeID, refreshedID.Extra["org_id"])
	assert.Equal(t, "acme", refreshedID.Extra["org_name"])

	w = postTokenForm(r, exchangeForm("user-123", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var plain map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &plain))
	assert.NotContains(t, plain, "refresh_token")
}

func TestTokenExchange_ClaimMappingApplies(t *testing.T) {
	r, ks, mappings, _ := newAuthRouterWithMappings(t)
	mappings.Set(map[string]string{"tenant": "tenant_id"})
	c := tokenClaims(t, ks, postTokenForm(r, exchangeForm("user-123", url.Values{"tenant": {"t1"}})))
	assert.Equal(t, "t1", c.Extra["tenant_id"])
}
