package auth0mock_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

			got, err := c.MFA.Get(context.Background())
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
			require.NoError(t, c.MFA.Set(context.Background(), tc.set))

			call := rec.last(t)
			assert.Equal(t, http.MethodPut, call.Method)
			assert.Equal(t, "/admin0/mfa-required", call.Path)
			assert.Equal(t, "application/json", call.ContentType)
			assert.JSONEq(t, tc.body, string(call.Body))
		})
	}
}
