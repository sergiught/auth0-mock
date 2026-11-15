package admin0

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func doMFAUsers(t *testing.T, d Deps, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	newRouter(d).ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func TestMFAUsers_PutGetDelete(t *testing.T) {
	d := newDeps()
	put := doMFAUsers(t, d, "PUT", "/admin0/mfa/users/alice",
		`{"required":true,"authenticators":[{"type":"otp"},{"type":"oob","oob_channel":"sms","active":false}]}`)
	require.Equal(t, 200, put.Code, put.Body.String())

	var body struct {
		User           string `json:"user"`
		Required       *bool  `json:"required"`
		Authenticators []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Active *bool  `json:"active"`
		} `json:"authenticators"`
	}
	require.NoError(t, json.Unmarshal(put.Body.Bytes(), &body))
	assert.Equal(t, "alice", body.User)
	require.NotNil(t, body.Required)
	assert.True(t, *body.Required)
	require.Len(t, body.Authenticators, 2)
	assert.NotEmpty(t, body.Authenticators[0].ID)
	require.NotNil(t, body.Authenticators[0].Active)
	assert.True(t, *body.Authenticators[0].Active)
	require.NotNil(t, body.Authenticators[1].Active)
	assert.False(t, *body.Authenticators[1].Active)

	get := doMFAUsers(t, d, "GET", "/admin0/mfa/users/alice", "")
	require.Equal(t, 200, get.Code)
	assert.JSONEq(t, put.Body.String(), get.Body.String())

	assert.Equal(t, 204, doMFAUsers(t, d, "DELETE", "/admin0/mfa/users/alice", "").Code)

	missing := doMFAUsers(t, d, "GET", "/admin0/mfa/users/alice", "")
	assert.Equal(t, 404, missing.Code)
	assert.Contains(t, missing.Body.String(), "unknown_user")
	assert.Equal(t, 404, doMFAUsers(t, d, "DELETE", "/admin0/mfa/users/alice", "").Code)
}

func TestMFAUsers_EscapedKey(t *testing.T) {
	d := newDeps()
	w := doMFAUsers(t, d, "PUT", "/admin0/mfa/users/auth0%7C123", `{"authenticators":[{"type":"otp"}]}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	_, ok := d.MFA.GetUser("auth0|123")
	assert.True(t, ok)
	assert.Contains(t, w.Body.String(), `"user":"auth0|123"`)
}

func TestMFAUsers_Validation(t *testing.T) {
	d := newDeps()
	w := doMFAUsers(t, d, "PUT", "/admin0/mfa/users/alice", `{"authenticators":[{"type":"sms"}]}`)
	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "type must be one of otp, oob, recovery-code")
	assert.Contains(t, w.Body.String(), "invalid_body")

	w = doMFAUsers(t, d, "PUT", "/admin0/mfa/users/alice", `{"bogus":1}`)
	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_body")
}

func TestMFAUsers_ListSorted(t *testing.T) {
	d := newDeps()
	assert.JSONEq(t, `[]`, doMFAUsers(t, d, "GET", "/admin0/mfa/users", "").Body.String())

	for _, u := range []string{"bob", "alice"} {
		require.Equal(t, 200, doMFAUsers(t, d, "PUT", "/admin0/mfa/users/"+u, `{"authenticators":[{"type":"otp"}]}`).Code)
	}
	w := doMFAUsers(t, d, "GET", "/admin0/mfa/users", "")
	require.Equal(t, 200, w.Code)
	var list []struct {
		User string `json:"user"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "alice", list[0].User)
	assert.Equal(t, "bob", list[1].User)
}

func TestMFAUsers_ResetClears(t *testing.T) {
	d := newDeps()
	require.Equal(t, 200, doMFAUsers(t, d, "PUT", "/admin0/mfa/users/alice", `{"authenticators":[{"type":"otp"}]}`).Code)
	require.Equal(t, 204, doMFAUsers(t, d, "POST", "/admin0/reset", "").Code)
	assert.JSONEq(t, `[]`, doMFAUsers(t, d, "GET", "/admin0/mfa/users", "").Body.String())
}

func TestMFAUsers_KeyDecodedOnce(t *testing.T) {
	cases := []struct{ path, key string }{
		{"50%25off", "50%off"},
		{"a%2Fb", "a/b"},
		{"a%2541", "a%41"},
		{"auth0%7C123", "auth0|123"},
		{"auth0|123", "auth0|123"},
		{"auth0%7c123", "auth0|123"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			d := newDeps()
			w := doMFAUsers(t, d, "PUT", "/admin0/mfa/users/"+c.path, `{"authenticators":[{"type":"otp"}]}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			_, ok := d.MFA.GetUser(c.key)
			assert.True(t, ok)
			assert.Equal(t, 200, doMFAUsers(t, d, "GET", "/admin0/mfa/users/"+c.path, "").Code)
		})
	}
}

func TestMFAUsers_EmptyAuthenticatorsSerialiseAsArray(t *testing.T) {
	d := newDeps()
	w := doMFAUsers(t, d, "PUT", "/admin0/mfa/users/alice", `{"required":true}`)
	require.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"authenticators":[]`)
}
