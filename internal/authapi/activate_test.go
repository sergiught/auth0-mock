package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
)

var stateRe = regexp.MustCompile(`name="state" value="([^"]+)"`)

func startDevice(t *testing.T, r chi.Router, form url.Values) (deviceCode, userCode string) {
	t.Helper()
	w := postDeviceCode(r, form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.DeviceCode, body.UserCode
}

func postActivate(r chi.Router, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func stateOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	m := stateRe.FindStringSubmatch(w.Body.String())
	require.NotNil(t, m, w.Body.String())
	return m[1]
}

// activateLogin runs the code and login steps and returns the confirm response.
func activateLogin(t *testing.T, r chi.Router, userCode string) *httptest.ResponseRecorder {
	t.Helper()
	w := postActivate(r, nil, url.Values{"code": {userCode}, "action": {"default"}})
	require.Equal(t, http.StatusOK, w.Code)
	return postActivate(r, nil, url.Values{
		"state": {stateOf(t, w)}, "username": {"jane@example.com"}, "password": {"x"}, "action": {"default"},
	})
}

func TestActivate_GetPrefill(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/activate?user_code=BCDF-GHJK", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `name="code"`)
	assert.Contains(t, w.Body.String(), `value="BCDF-GHJK"`)
	assert.Contains(t, w.Body.String(), "Activate your device")
	assert.Contains(t, w.Body.String(), "<style>\nbody{")
}

func TestLoginPage_StylesheetIntact(t *testing.T) {
	w := httptest.NewRecorder()
	renderLoginPage(w, http.StatusOK, "app", "", "")
	assert.Contains(t, w.Body.String(), "<style>\nbody{")
}

func TestActivate_ConfirmStateWithLoginAction(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	dc, uc := startDevice(t, r, url.Values{"client_id": {"app"}})
	w := activateLogin(t, r, uc)
	st := stateOf(t, w)
	w = postActivate(r, nil, url.Values{"state": {st}, "username": {"mallory@example.com"}, "password": {"x"}, "action": {"default"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, pollDevice(r, dc, "app").Body.String(), "authorization_pending")
}

func TestActivate_GetEscapes(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/activate?user_code="+url.QueryEscape(`"><script>x</script>`), nil))
	assert.NotContains(t, w.Body.String(), "<script>x")
}

func TestActivate_FullRoundTrip(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	dc, uc := startDevice(t, r, url.Values{"client_id": {"app"}})

	loose := strings.ToLower(strings.ReplaceAll(uc, "-", ""))
	w := postActivate(r, nil, url.Values{"code": {loose}, "action": {"default"}})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `name="username"`)
	loginState := stateOf(t, w)

	w = postActivate(r, nil, url.Values{"state": {loginState}, "username": {"jane@example.com"}, "password": {"x"}, "action": {"default"}})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Device Confirmation")
	assert.Contains(t, w.Body.String(), uc)
	assert.NotEmpty(t, loginCookieOf(t, w).Value)

	w = postActivate(r, nil, url.Values{"state": {stateOf(t, w)}, "action": {"confirm"}})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Congratulations, you're all set!")

	w = pollDevice(r, dc, "app")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tok struct {
		IDToken string `json:"id_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tok))
	assert.Equal(t, "jane@example.com", rawClaims(t, tok.IDToken)["sub"])
}

func TestActivate_LiveSessionSkipsLogin(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	_, uc := startDevice(t, r, url.Values{"client_id": {"app"}})
	w := activateLogin(t, r, uc)
	cookie := loginCookieOf(t, w)

	dc2, uc2 := startDevice(t, r, url.Values{"client_id": {"app"}})
	w = postActivate(r, cookie, url.Values{"code": {uc2}, "action": {"default"}})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Device Confirmation")
	assert.NotContains(t, w.Body.String(), `name="username"`)

	w = postActivate(r, cookie, url.Values{"state": {stateOf(t, w)}, "action": {"confirm"}})
	require.Equal(t, http.StatusOK, w.Code)
	w = pollDevice(r, dc2, "app")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tok struct {
		IDToken string `json:"id_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tok))
	assert.Equal(t, "jane@example.com", rawClaims(t, tok.IDToken)["sub"])
}

func TestActivate_Cancel(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	dc, uc := startDevice(t, r, url.Values{"client_id": {"app"}})
	w := activateLogin(t, r, uc)
	w = postActivate(r, nil, url.Values{"state": {stateOf(t, w)}, "action": {"cancel"}})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Device activation cancelled")
	w = pollDevice(r, dc, "app")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "access_denied")
}

func TestActivate_BadCode(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	w := postActivate(r, nil, url.Values{"code": {"ZZZZ-ZZZZ"}, "action": {"default"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid or expired code")
	assert.Contains(t, w.Body.String(), `name="code"`)
	assert.Contains(t, w.Body.String(), `value="ZZZZ-ZZZZ"`)
}

func TestActivate_EmptyFieldsAndRule(t *testing.T) {
	r, _, _, errs := newLoginPageRouter(t, true)
	errs.Add(authorizeerr.Rule{Match: map[string]string{"username": "locked@example.com"}, Error: "access_denied"})
	_, uc := startDevice(t, r, url.Values{"client_id": {"app"}})
	w := postActivate(r, nil, url.Values{"code": {uc}, "action": {"default"}})
	st := stateOf(t, w)

	w = postActivate(r, nil, url.Values{"state": {st}, "password": {"x"}, "action": {"default"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Email is required")
	assert.Equal(t, st, stateOf(t, w))

	w = postActivate(r, nil, url.Values{"state": {st}, "username": {"a@b.c"}, "action": {"default"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Password is required")

	w = postActivate(r, nil, url.Values{"state": {st}, "username": {"locked@example.com"}, "password": {"x"}, "action": {"default"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Wrong email or password.")

	w = postActivate(r, nil, url.Values{"state": {st}, "username": {"jane@example.com"}, "password": {"x"}, "action": {"default"}})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Device Confirmation")
}

func TestActivate_StepReplay(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	dc, uc := startDevice(t, r, url.Values{"client_id": {"app"}})
	w := postActivate(r, nil, url.Values{"code": {uc}, "action": {"default"}})
	st := stateOf(t, w)
	login := url.Values{"state": {st}, "username": {"jane@example.com"}, "password": {"x"}, "action": {"default"}}
	assert.Equal(t, http.StatusOK, postActivate(r, nil, login).Code)
	assert.Equal(t, http.StatusBadRequest, postActivate(r, nil, login).Code)

	dc2, uc2 := startDevice(t, r, url.Values{"client_id": {"app"}})
	w = postActivate(r, nil, url.Values{"code": {uc2}, "action": {"default"}})
	w2 := postActivate(r, nil, url.Values{"state": {stateOf(t, w)}, "action": {"confirm"}})
	assert.Equal(t, http.StatusBadRequest, w2.Code)
	assert.Contains(t, pollDevice(r, dc2, "app").Body.String(), "authorization_pending")

	w = pollDevice(r, dc, "app")
	assert.Contains(t, w.Body.String(), "authorization_pending")
	assert.Equal(t, http.StatusBadRequest, postActivate(r, nil, url.Values{"action": {"bogus"}}).Code)
}

func TestActivate_ExpiresMidway(t *testing.T) {
	r, _, clk, _ := newLoginPageRouter(t, true)
	dc, uc := startDevice(t, r, url.Values{"client_id": {"app"}})
	w := activateLogin(t, r, uc)
	st := stateOf(t, w)
	clk.Advance(901 * time.Second)
	w = postActivate(r, nil, url.Values{"state": {st}, "action": {"confirm"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = pollDevice(r, dc, "app")
	assert.Contains(t, w.Body.String(), "expired_token")
}
