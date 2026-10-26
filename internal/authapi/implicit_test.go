package authapi

import (
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
)

const implicitBase = "client_id=demo&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&"

func fragmentOf(t *testing.T, r chi.Router, query string) url.Values {
	t.Helper()
	u := location(t, getAuthorize(r, query))
	assert.False(t, u.Query().Has("access_token"))
	frag, err := url.ParseQuery(u.Fragment)
	require.NoError(t, err)
	return frag
}

func verified(t *testing.T, ks *jwks.KeySet, tok string) *jwks.Claims {
	t.Helper()
	c, err := ks.Verify(tok, jwks.VerifyOpts{})
	require.NoError(t, err)
	return c
}

func TestAuthorize_TokenReturnsJWT(t *testing.T) {
	r, ks := newAuthRouter(t)
	frag := fragmentOf(t, r, implicitBase+"response_type=token&scope=openid%20read")
	c := verified(t, ks, frag.Get("access_token"))
	assert.Equal(t, "Bearer", frag.Get("token_type"))
	assert.Equal(t, "3600", frag.Get("expires_in"))
	assert.Equal(t, "openid read", frag.Get("scope"))
	assert.Equal(t, "s1", frag.Get("state"))
	assert.False(t, frag.Has("id_token"))
	assert.False(t, frag.Has("code"))
	assert.False(t, frag.Has("refresh_token"))
	assert.Contains(t, c.Audience, "https://mock/api/v2/")
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.Equal(t, "auth0|mock-user", c.Subject)
}

func TestAuthorize_IDTokenOnly(t *testing.T) {
	r, ks := newAuthRouter(t)
	frag := fragmentOf(t, r, implicitBase+"response_type=id_token&nonce=n1")
	assert.False(t, frag.Has("access_token"))
	assert.False(t, frag.Has("code"))
	c := verified(t, ks, frag.Get("id_token"))
	assert.Equal(t, []string{"demo"}, c.Audience)
	assert.Equal(t, "n1", c.Extra["nonce"])
	assert.Equal(t, "demo", c.Extra["azp"])
	assert.Contains(t, c.Extra, "auth_time")
	assert.Equal(t, deriveProfile("auth0|mock-user")["email"], c.Extra["email"])
	assert.NotContains(t, c.Extra, "at_hash")
}

func TestAuthorize_IDTokenTokenWithLoginHint(t *testing.T) {
	r, ks, _, _, _ := newRefreshRouterWithSessions(t)
	frag := fragmentOf(t, r, implicitBase+"response_type=id_token%20token&nonce=n1&login_hint=bob@example.com&audience=https%3A%2F%2Fapi%2F")
	access := verified(t, ks, frag.Get("access_token"))
	id := verified(t, ks, frag.Get("id_token"))
	assert.Contains(t, access.Audience, "https://api/")
	assert.Equal(t, "bob@example.com", access.Subject)
	assert.Equal(t, "bob@example.com", id.Subject)
	assert.Equal(t, tokenHash(frag.Get("access_token")), id.Extra["at_hash"])
	assert.Equal(t, "bob@example.com", getUserinfo(t, r, frag.Get("access_token"))["email"])
}

func TestAuthorize_HybridHashes(t *testing.T) {
	r, ks, _, _, _ := newRefreshRouterWithSessions(t)
	frag := fragmentOf(t, r, implicitBase+"response_type=code%20id_token%20token&nonce=n1")
	code := frag.Get("code")
	require.NotEmpty(t, code)
	id := verified(t, ks, frag.Get("id_token"))
	assert.Equal(t, tokenHash(code), id.Extra["c_hash"])
	assert.Equal(t, tokenHash(frag.Get("access_token")), id.Extra["at_hash"])
	verified(t, ks, frag.Get("access_token"))

	resp := decodeToken(t, exchangeCode(r, code, ""))
	exchanged := verified(t, ks, resp.IDToken)
	assert.Equal(t, "n1", exchanged.Extra["nonce"])
}

func TestAuthorize_ResponseTypeOrderInsensitive(t *testing.T) {
	r, _ := newAuthRouter(t)
	for _, rt := range []string{"id_token%20token", "token%20id_token", "token%20%20id_token%20token"} {
		frag := fragmentOf(t, r, implicitBase+"nonce=n1&response_type="+rt)
		assert.NotEmpty(t, frag.Get("access_token"), rt)
		assert.NotEmpty(t, frag.Get("id_token"), rt)
	}
}

func TestAuthorize_UnknownResponseType(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := getAuthorize(r, implicitBase+"response_type=cod")
	assert.Equal(t, "https://app/cb?error=unsupported_response_type&error_description=Unsupported+response+type%3A+cod&state=s1",
		location(t, w).String())
}

func TestAuthorize_IDTokenNeedsNonce(t *testing.T) {
	r, _ := newAuthRouter(t)
	frag := fragmentOf(t, r, implicitBase+"response_type=id_token")
	assert.Equal(t, "invalid_request", frag.Get("error"))
	assert.Equal(t, "nonce is required when response_type includes id_token", frag.Get("error_description"))
}

func TestAuthorize_TokenNeedsClientID(t *testing.T) {
	r, _ := newAuthRouter(t)
	frag := fragmentOf(t, r, "redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=token")
	assert.Equal(t, "invalid_request", frag.Get("error"))
	assert.Equal(t, "Missing required parameter: client_id", frag.Get("error_description"))
}

func TestAuthorize_CodeUnchanged(t *testing.T) {
	r, _ := newAuthRouter(t)
	for _, q := range []string{implicitBase + "response_type=code", implicitBase} {
		u := location(t, getAuthorize(r, q))
		assert.NotEmpty(t, u.Query().Get("code"), q)
		assert.Empty(t, u.Fragment, q)
	}
}

func TestAuthorize_FormPostCarriesTokens(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := getAuthorize(r, implicitBase+"response_type=id_token%20token&nonce=n1&response_mode=form_post")
	require.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `name="access_token"`)
	assert.Contains(t, w.Body.String(), `name="id_token"`)
}

func TestAuthorize_ErrorRuleBeatsImplicit(t *testing.T) {
	r, errs, _ := newAuthRouterWithErrors(t)
	addDeniedRule(errs, nil, "")
	frag := fragmentOf(t, r, implicitBase+"response_type=id_token%20token&nonce=n1")
	assert.Equal(t, "access_denied", frag.Get("error"))
	assert.False(t, frag.Has("access_token"))
}

func TestAuthorize_UnknownResponseTypeBeatsBadChallenge(t *testing.T) {
	r, _ := newAuthRouter(t)
	u := location(t, getAuthorize(r, implicitBase+"response_type=cod&code_challenge=short"))
	assert.Equal(t, "unsupported_response_type", u.Query().Get("error"))
}
