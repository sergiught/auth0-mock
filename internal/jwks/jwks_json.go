package jwks

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
)

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

// JWKSJSON returns the JWKS document for /.well-known/jwks.json.
func (k *KeySet) JWKSJSON() []byte {
	pub := k.PublicKey()
	// RSA public exponents are tiny positive ints (typically 65537) — Go's
	// crypto/rsa generates only odd primes with E in {3, 17, 65537}, so the
	// int → uint32 conversion is always safe; the explicit clamp is
	// defence-in-depth.
	e := pub.E
	if e < 0 || e > 0xFFFFFFFF {
		e = 65537
	}
	eBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(eBytes, uint32(e))
	// Trim leading zero bytes.
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	doc := jwks{Keys: []jwk{{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: k.keyID,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}}}
	out, _ := json.Marshal(doc)
	return out
}
