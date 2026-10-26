package authapi

import (
	"crypto/sha256"
	"encoding/base64"
)

// tokenHash computes the OIDC at_hash / c_hash value (OIDC Core section
// 3.3.2.11): the base64url encoding of the left half of the SHA-256 digest,
// matching the RS256 signing algorithm.
func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:len(sum)/2])
}
