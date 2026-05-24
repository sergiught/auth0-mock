package pkce

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// FuzzEntryVerify exercises the PKCE verifier comparison. The verifier arrives
// on /oauth/token from the client, so it's attacker-influenced and Verify must
// stay panic-free for any byte sequence. Beyond that, the test asserts the
// S256 and plain round-trips: a verifier hashed into its own challenge must
// verify, and an empty verifier must always be rejected.
func FuzzEntryVerify(f *testing.F) {
	f.Add("dummy-verifier")
	f.Add("")
	f.Add("a")
	for _, seed := range []string{"短い", "with spaces", "tab\tand\nnewline", "/+=padding"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, verifier string) {
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])

		s256 := &Entry{Challenge: challenge, Method: MethodS256}
		if verifier == "" {
			// An empty verifier is rejected outright, whatever the challenge.
			if err := s256.Verify(verifier); err == nil {
				t.Fatal("empty verifier should not verify")
			}
			return
		}

		// Round-trip: the correct verifier matches its own S256 challenge.
		if err := s256.Verify(verifier); err != nil {
			t.Fatalf("S256 round-trip failed for %q: %v", verifier, err)
		}

		// Plain method: the verifier must equal the stored challenge verbatim.
		plain := &Entry{Challenge: verifier, Method: MethodPlain}
		if err := plain.Verify(verifier); err != nil {
			t.Fatalf("plain round-trip failed for %q: %v", verifier, err)
		}
	})
}
