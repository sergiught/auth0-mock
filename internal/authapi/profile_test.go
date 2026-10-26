package authapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveProfile(t *testing.T) {
	tests := []struct{ subject, email, nickname, picture string }{
		{"alice@example.com", "alice@example.com", "alice", "https://cdn.auth0.com/avatars/al.png"},
		{"auth0|mock-user", "mock-user@example.com", "mock-user", "https://cdn.auth0.com/avatars/mo.png"},
		{"google-oauth2|123", "123@example.com", "123", "https://cdn.auth0.com/avatars/12.png"},
		{"élodie@example.com", "élodie@example.com", "élodie", "https://cdn.auth0.com/avatars/él.png"},
		{"Bob@example.com", "Bob@example.com", "Bob", "https://cdn.auth0.com/avatars/bo.png"},
		{"x", "x@example.com", "x", "https://cdn.auth0.com/avatars/x.png"},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			p := deriveProfile(tt.subject)
			assert.Equal(t, tt.email, p["email"])
			assert.Equal(t, tt.email, p["name"])
			assert.Equal(t, tt.nickname, p["nickname"])
			assert.Equal(t, tt.picture, p["picture"])
			assert.Equal(t, true, p["email_verified"])
		})
	}
}

func TestFillProfile_KeepsExistingKeys(t *testing.T) {
	got := fillProfile(map[string]any{"email": "keep@x.io", "azp": "demo"}, "alice@example.com")
	assert.Equal(t, "keep@x.io", got["email"])
	assert.Equal(t, "demo", got["azp"])
	assert.Equal(t, "alice", got["nickname"])
	assert.Equal(t, "alice@example.com", got["name"])
}
