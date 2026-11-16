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

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

func postDeviceCode(r chi.Router, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/oauth/device/code", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestDeviceCode_Response(t *testing.T) {
	r, store, clk, _ := newLoginPageRouter(t, true)
	w := postDeviceCode(r, url.Values{"client_id": {"app"}, "scope": {"openid offline_access"}, "audience": {"https://api"}})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	raw := w.Body.String()
	last := -1
	for _, k := range []string{"device_code", "user_code", "verification_uri", "verification_uri_complete", "expires_in", "interval"} {
		i := strings.Index(raw, `"`+k+`":`)
		require.Greater(t, i, last, k)
		last = i
	}
	assert.NotEmpty(t, body.DeviceCode)
	assert.Regexp(t, `^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`, body.UserCode)
	assert.Equal(t, "https://mock/activate", body.VerificationURI)
	assert.Equal(t, "https://mock/activate?user_code="+body.UserCode, body.VerificationURIComplete)
	assert.Equal(t, 900, body.ExpiresIn)
	assert.Equal(t, 5, body.Interval)

	d, ok := store.DeviceByUserCode(body.UserCode, clk.Now())
	require.True(t, ok)
	assert.Equal(t, "app", d.ClientID)
	assert.Equal(t, "https://api", d.Audience)
	assert.Equal(t, "openid offline_access", d.Scope)
	assert.Equal(t, clk.Now().Add(deviceCodeTTL), d.Expires)
}

func TestDeviceCode_DefaultAudience(t *testing.T) {
	r, store, clk, _ := newLoginPageRouter(t, true)
	w := postDeviceCode(r, url.Values{"client_id": {"app"}})
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		UserCode string `json:"user_code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	d, ok := store.DeviceByUserCode(body.UserCode, clk.Now())
	require.True(t, ok)
	assert.Equal(t, "https://mock/api/v2/", d.Audience)
}

func TestDeviceCode_MissingClientID(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	w := postDeviceCode(r, url.Values{"scope": {"openid"}})
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "invalid_request", body["error"])
	assert.Equal(t, "Missing required parameter: client_id", body["error_description"])
}

func TestDeviceCode_PageOffApproved(t *testing.T) {
	r, store, clk, _ := newLoginPageRouter(t, false)
	w := postDeviceCode(r, url.Values{"client_id": {"app"}})
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// Page off creates the entry already approved for the default subject.
	d, res := store.PollDevice(body.DeviceCode, "app", clk.Now())
	require.Equal(t, session.PollApproved, res)
	assert.Equal(t, DefaultSubject, d.Subject)
	assert.Equal(t, clk.Now(), d.AuthTime)
}

func TestDeviceCode_PageOnPending(t *testing.T) {
	r, store, clk, _ := newLoginPageRouter(t, true)
	w := postDeviceCode(r, url.Values{"client_id": {"app"}})
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	_, res := store.PollDevice(body.DeviceCode, "app", clk.Now())
	assert.Equal(t, session.PollPending, res)
}

func TestDeviceCode_ActivateMountedOnlyWithPage(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, false)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/activate", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestMount_DeviceStoreShared(t *testing.T) {
	r, _ := newAuthRouter(t)
	var tok *TokenHandler
	var dev *DeviceCodeHandler
	_ = chi.Walk(r, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch v := h.(type) {
		case *TokenHandler:
			tok = v
		case *DeviceCodeHandler:
			dev = v
		}
		return nil
	})
	require.NotNil(t, tok)
	require.NotNil(t, dev)
	require.NotNil(t, tok.Devices)
	assert.Same(t, tok.Devices, dev.Devices)
}

func TestDeviceCode_IssuerWithoutTrailingSlash(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "http://host:8080", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "http://host:8080", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(), Sessions: session.NewStore(),
		AuthorizeErrors: authorizeerr.NewStore(), LoginPage: true,
	})
	w := postDeviceCode(r, url.Values{"client_id": {"app"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body deviceCodeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "http://host:8080/activate", body.VerificationURI)
	assert.Equal(t, "http://host:8080/activate?user_code="+body.UserCode, body.VerificationURIComplete)
}
