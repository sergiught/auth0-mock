package authapi

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenHash_KnownVector(t *testing.T) {
	sum := sha256.Sum256([]byte("abc"))
	want := base64.RawURLEncoding.EncodeToString(sum[:16])
	assert.Equal(t, want, tokenHash("abc"))
	assert.Equal(t, "ungWv48Bz-pBQUDeXa4iIw", tokenHash("abc"))
}
