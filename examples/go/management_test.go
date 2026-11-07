package example

import (
	"context"
	"net/url"
	"testing"

	client "github.com/auth0/go-auth0/v3/management/client"
	"github.com/auth0/go-auth0/v3/management/option"
)

func TestSDKReadsAStubbedUser(t *testing.T) {
	stub(t, map[string]any{
		"method": "GET",
		"path":   "/api/v2/users/auth0|123",
		"response": map[string]any{
			"status": 200,
			"body":   map[string]string{"user_id": "auth0|123", "email": "alice@example.com"},
		},
	})

	mock, err := url.Parse(mockURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mgmt, err := client.New(mock.Host, option.WithClientCredentials(ctx, "demo", "x"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := mgmt.Users.Get(ctx, "auth0|123", nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := *user.UserID; got != "auth0|123" {
		t.Fatalf("user_id = %q", got)
	}
	if got := *user.Email; got != "alice@example.com" {
		t.Fatalf("email = %q", got)
	}
}
