package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

func pollDevice(r chi.Router, deviceCode, clientID string) *httptest.ResponseRecorder {
	form := url.Values{"grant_type": {grantDeviceCode}}
	if deviceCode != "" {
		form.Set("device_code", deviceCode)
	}
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	return postTokenForm(r, form)
}

func requireAuthError(t *testing.T, w *httptest.ResponseRecorder, status int, code, desc string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var body struct{ Error, ErrorDescription string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &struct {
		Error            *string `json:"error"`
		ErrorDescription *string `json:"error_description"`
	}{&body.Error, &body.ErrorDescription}))
	assert.Equal(t, code, body.Error)
	assert.Equal(t, desc, body.ErrorDescription)
}

func TestDeviceGrant_PageOffFirstPoll(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, false)
	dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}, "scope": {"openid offline_access"}, "audience": {"https://api"}})

	w := pollDevice(r, dc, "app")
	resp := decodeToken(t, w)
	access := rawClaims(t, resp.AccessToken)
	assert.Equal(t, "auth0|mock-user", access["sub"])
	assert.Contains(t, access["aud"], "https://api")
	assert.Equal(t, "app", access["azp"])
	assert.NotContains(t, access, "gty")
	id := rawClaims(t, resp.IDToken)
	assert.Contains(t, id["aud"], "app")
	assert.Contains(t, id, "auth_time")
	assert.NotEmpty(t, resp.RefreshToken)

	requireAuthError(t, pollDevice(r, dc, "app"), http.StatusForbidden, "invalid_grant", "Invalid or expired device code.")
}

func TestDeviceGrant_MatchesAuthorizationCode(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, false)
	scope := "openid profile offline_access"
	audience := "https://api"

	loc, err := url.Parse(loginPageAuthorize(r, nil, url.Values{"scope": {scope}, "audience": {audience}}).Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	viaCode := decodeToken(t, postTokenForm(r, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"app"}, "code": {code}, "redirect_uri": {"https://app/cb"},
	}))
	dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}, "scope": {scope}, "audience": {audience}})
	viaDevice := decodeToken(t, pollDevice(r, dc, "app"))

	norm := func(m map[string]any) map[string]any {
		delete(m, "jti")
		delete(m, "gty")
		return m
	}
	assert.Equal(t, norm(rawClaims(t, viaCode.AccessToken)), norm(rawClaims(t, viaDevice.AccessToken)))
	assert.Equal(t, norm(rawClaims(t, viaCode.IDToken)), norm(rawClaims(t, viaDevice.IDToken)))
	assert.Equal(t, viaCode.Scope, viaDevice.Scope)
	assert.Equal(t, viaCode.ExpiresIn, viaDevice.ExpiresIn)
	assert.Equal(t, "authorization-code", rawClaims(t, viaCode.AccessToken)["gty"])
}

func TestDeviceGrant_StrictRefreshNeedsOfflineAccess(t *testing.T) {
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(pkce.WithNow(clk.Now)), TokenEndpointStrict: true,
	})
	withOffline, _ := startDevice(t, r, url.Values{"client_id": {"app"}, "scope": {"openid offline_access"}})
	assert.NotEmpty(t, decodeToken(t, pollDevice(r, withOffline, "app")).RefreshToken)
	without, _ := startDevice(t, r, url.Values{"client_id": {"app"}, "scope": {"openid"}})
	assert.Empty(t, decodeToken(t, pollDevice(r, without, "app")).RefreshToken)
}

func TestDeviceGrant_Errors(t *testing.T) {
	const invalid = "Invalid or expired device code."
	tests := []struct {
		name   string
		poll   func(t *testing.T, r chi.Router, store *session.Store, clk *steppedClock) *httptest.ResponseRecorder
		status int
		code   string
		desc   string
	}{
		{
			name: "missing device_code",
			poll: func(_ *testing.T, r chi.Router, _ *session.Store, _ *steppedClock) *httptest.ResponseRecorder {
				return pollDevice(r, "", "app")
			},
			status: 400, code: "invalid_request", desc: "Missing required parameter: device_code",
		},
		{
			name: "unknown device_code",
			poll: func(_ *testing.T, r chi.Router, _ *session.Store, _ *steppedClock) *httptest.ResponseRecorder {
				return pollDevice(r, "nope", "app")
			},
			status: 403, code: "invalid_grant", desc: invalid,
		},
		{
			name: "client mismatch",
			poll: func(t *testing.T, r chi.Router, _ *session.Store, _ *steppedClock) *httptest.ResponseRecorder {
				dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}})
				return pollDevice(r, dc, "other")
			},
			status: 403, code: "invalid_grant", desc: invalid,
		},
		{
			name: "pending",
			poll: func(t *testing.T, r chi.Router, _ *session.Store, _ *steppedClock) *httptest.ResponseRecorder {
				dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}})
				return pollDevice(r, dc, "app")
			},
			status: 403, code: "authorization_pending", desc: "User has yet to authorize device code.",
		},
		{
			name: "denied, and still denied on the next poll",
			poll: func(t *testing.T, r chi.Router, store *session.Store, _ *steppedClock) *httptest.ResponseRecorder {
				dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}})
				require.True(t, store.DenyDevice(dc))
				requireAuthError(t, pollDevice(r, dc, "app"), 403, "access_denied", "User cancelled the confirmation prompt.")
				return pollDevice(r, dc, "app")
			},
			status: 403, code: "access_denied", desc: "User cancelled the confirmation prompt.",
		},
		{
			name: "expired, then invalid_grant",
			poll: func(t *testing.T, r chi.Router, _ *session.Store, clk *steppedClock) *httptest.ResponseRecorder {
				dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}})
				clk.Advance(900 * time.Second)
				requireAuthError(t, pollDevice(r, dc, "app"), 403, "expired_token", "The device code has expired.")
				return pollDevice(r, dc, "app")
			},
			status: 403, code: "invalid_grant", desc: invalid,
		},
		{
			name: "still valid just before expiry",
			poll: func(t *testing.T, r chi.Router, _ *session.Store, clk *steppedClock) *httptest.ResponseRecorder {
				dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}})
				clk.Advance(900*time.Second - time.Second)
				return pollDevice(r, dc, "app")
			},
			status: 403, code: "authorization_pending", desc: "User has yet to authorize device code.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, store, clk, _ := newLoginPageRouter(t, true)
			requireAuthError(t, tc.poll(t, r, store, clk), tc.status, tc.code, tc.desc)
		})
	}
}

func TestDeviceGrant_ApprovedSubject(t *testing.T) {
	r, store, clk, _ := newLoginPageRouter(t, true)
	dc, _ := startDevice(t, r, url.Values{"client_id": {"app"}, "scope": {"openid email"}})
	authTime := clk.Now().Add(time.Minute)
	require.True(t, store.ApproveDevice(dc, "jane@example.com", authTime))

	resp := decodeToken(t, pollDevice(r, dc, "app"))
	assert.Equal(t, "jane@example.com", rawClaims(t, resp.AccessToken)["sub"])
	id := rawClaims(t, resp.IDToken)
	assert.Equal(t, "jane@example.com", id["sub"])
	assert.Equal(t, "jane@example.com", id["email"])
	assert.EqualValues(t, authTime.Unix(), id["auth_time"])
}
