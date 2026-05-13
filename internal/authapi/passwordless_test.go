package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordless_StartEmail_200(t *testing.T) {
	r, _ := newAuthRouter(t)
	body := `{"client_id":"abc","connection":"email","email":"alice@example.com","send":"code"}`
	req := httptest.NewRequest("POST", "/passwordless/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["_id"])
	assert.Equal(t, "email", resp["email"])
}

func TestPasswordless_VerifyAfterStart_MintsToken(t *testing.T) {
	r, _ := newAuthRouter(t)

	// Start.
	startBody := `{"client_id":"abc","connection":"email","email":"alice@example.com","send":"code"}`
	startReq := httptest.NewRequest("POST", "/passwordless/start", strings.NewReader(startBody))
	startReq.Header.Set("Content-Type", "application/json")
	startW := httptest.NewRecorder()
	r.ServeHTTP(startW, startReq)
	require.Equal(t, http.StatusOK, startW.Code)

	// In a real provider, the user receives the OTP out-of-band. The mock
	// always accepts the literal code "000000".
	form := url.Values{}
	form.Set("grant_type", "http://auth0.com/oauth/grant-type/passwordless/otp")
	form.Set("client_id", "abc")
	form.Set("realm", "email")
	form.Set("username", "alice@example.com")
	form.Set("otp", "000000")

	verReq := httptest.NewRequest("POST", "/passwordless/verify", strings.NewReader(form.Encode()))
	verReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	verW := httptest.NewRecorder()
	r.ServeHTTP(verW, verReq)

	require.Equal(t, http.StatusOK, verW.Code)
	var verResp tokenResponse
	require.NoError(t, json.Unmarshal(verW.Body.Bytes(), &verResp))
	assert.NotEmpty(t, verResp.AccessToken)
}

func TestPasswordless_Verify_WrongOTP_403(t *testing.T) {
	r, _ := newAuthRouter(t)

	form := url.Values{}
	form.Set("grant_type", "http://auth0.com/oauth/grant-type/passwordless/otp")
	form.Set("client_id", "abc")
	form.Set("realm", "email")
	form.Set("username", "alice@example.com")
	form.Set("otp", "wrong")
	req := httptest.NewRequest("POST", "/passwordless/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
