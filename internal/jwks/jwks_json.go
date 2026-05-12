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
	eBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(eBytes, uint32(pub.E))
	// trim leading zero bytes
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
