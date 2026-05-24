package auth0mock_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  auth0mock.APIError
		want string
	}{
		{
			name: "message and code",
			err:  auth0mock.APIError{StatusCode: 400, Reason: "Bad Request", Message: "bad", ErrorCode: "invalid_body"},
			want: "auth0mock: 400 invalid_body: bad",
		},
		{
			name: "message only",
			err:  auth0mock.APIError{StatusCode: 400, Reason: "Bad Request", Message: "bad"},
			want: "auth0mock: 400 Bad Request: bad",
		},
		{
			name: "code only",
			err:  auth0mock.APIError{StatusCode: 404, ErrorCode: "unknown_id"},
			want: "auth0mock: 404 unknown_id",
		},
		{
			name: "reason only",
			err:  auth0mock.APIError{StatusCode: 403, Reason: "Forbidden"},
			want: "auth0mock: 403 Forbidden",
		},
		{
			name: "empty envelope falls back to status text",
			err:  auth0mock.APIError{StatusCode: 500},
			want: "auth0mock: 500 " + http.StatusText(500),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, c.err.Error())
		})
	}
}
