module github.com/sergiught/auth0-mock/examples/sdk

go 1.26.5

require github.com/sergiught/auth0-mock v0.231.0

require (
	github.com/PuerkitoBio/rehttp v1.4.0 // indirect
	github.com/auth0/go-auth0 v1.45.0
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.6 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/jwx/v2 v2.1.7 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	go.devnw.com/structs v1.0.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// Local-path replace so `go run .` from this directory picks up the
// in-tree SDK rather than the (eventually-published) module from the
// proxy. Downstream consumers copying this example into their own
// project should drop the replace and pin a real version instead, e.g.
//   require github.com/sergiught/auth0-mock v0.226.0
replace github.com/sergiught/auth0-mock => ../..
