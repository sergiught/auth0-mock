package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	// The page-off router has a store of its own, so read it back via Mount's
	// shared Sessions.
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
