package auth0mock_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestMFA_IsRequired(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"required true", `{"required":true}`, true},
		{"required false", `{"required":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, c := newStub(t)
			rec.respond = func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}

			got, err := c.MFA.Get(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			call := rec.last(t)
			assert.Equal(t, http.MethodGet, call.Method)
			assert.Equal(t, "/admin0/mfa-required", call.Path)
		})
	}
}

func TestMFA_SetRequired_WireShape(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		set  bool
		body string
	}{
		{"set true", true, `{"required":true}`},
		{"set false", false, `{"required":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, c := newStub(t)
			require.NoError(t, c.MFA.Set(t.Context(), tc.set))

			call := rec.last(t)
			assert.Equal(t, http.MethodPut, call.Method)
			assert.Equal(t, "/admin0/mfa-required", call.Path)
			assert.Equal(t, "application/json", call.ContentType)
			assert.JSONEq(t, tc.body, string(call.Body))
		})
	}
}

func TestMFAUsers_SDKRoundTrip(t *testing.T) {
	t.Parallel()
	_, c := newNamespaceServer(t)
	ctx := t.Context()

	set, err := c.MFA.SetUser(ctx, "auth0|123", auth0mock.MFAUser{
		Authenticators: []auth0mock.MFAAuthenticator{{Type: "otp"}},
	})
	require.NoError(t, err)
	require.Len(t, set.Authenticators, 1)
	assert.NotEmpty(t, set.Authenticators[0].ID)
	require.NotNil(t, set.Authenticators[0].Active)
	assert.True(t, *set.Authenticators[0].Active)

	got, err := c.MFA.GetUser(ctx, "auth0|123")
	require.NoError(t, err)
	assert.Equal(t, set, got)

	list, err := c.MFA.ListUsers(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, c.MFA.DeleteUser(ctx, "auth0|123"))
	_, err = c.MFA.GetUser(ctx, "auth0|123")
	var apiErr *auth0mock.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

func TestMFAUsers_SDKValidationError(t *testing.T) {
	t.Parallel()
	_, c := newNamespaceServer(t)
	_, err := c.MFA.SetUser(t.Context(), "auth0|123", auth0mock.MFAUser{
		Authenticators: []auth0mock.MFAAuthenticator{{Type: "bogus"}},
	})
	var apiErr *auth0mock.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}
