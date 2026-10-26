package authapi

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestPasswordlessUser_Email(t *testing.T) {
	sub, p := passwordlessUser("email", "alice@example.com")
	sum := sha256.Sum256([]byte("alice@example.com"))
	assert.Regexp(t, `^email\|[0-9a-f]{24}$`, sub)
	assert.Equal(t, "email|"+hex.EncodeToString(sum[:])[:24], sub)
	assert.Equal(t, "alice@example.com", p["email"])
	assert.Equal(t, true, p["email_verified"])
	assert.Equal(t, "alice@example.com", p["name"])
	assert.Equal(t, "alice", p["nickname"])
	assert.Equal(t, "https://cdn.auth0.com/avatars/al.png", p["picture"])
	assert.NotContains(t, p, "phone_number")
}

func TestPasswordlessUser_SMS(t *testing.T) {
	sub, p := passwordlessUser("sms", "+15551234567")
	assert.Equal(t, "sms|", sub[:4])
	assert.Len(t, sub, 28)
	assert.Equal(t, "+15551234567", p["phone_number"])
	assert.Equal(t, true, p["phone_number_verified"])
	assert.Equal(t, "+15551234567", p["name"])
	assert.Equal(t, "+15551234567", p["nickname"])
	assert.Equal(t, "https://cdn.auth0.com/avatars/default.png", p["picture"])
	assert.NotContains(t, p, "email")
}

func TestPasswordlessUser_StableAcrossCaseAndSpace(t *testing.T) {
	a, _ := passwordlessUser("email", " Alice@Example.com ")
	b, _ := passwordlessUser("email", "alice@example.com")
	assert.Equal(t, b, a)
}

func TestProfileOf_KeepsPhoneFields(t *testing.T) {
	p := profileOf(map[string]any{"phone_number": "+1", "phone_number_verified": true, "azp": "x"})
	require.Equal(t, map[string]any{"phone_number": "+1", "phone_number_verified": true}, p)
}
