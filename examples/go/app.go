package example

import (
	"encoding/json"
	"net/http"
	"net/url"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v3"
	"github.com/auth0/go-jwt-middleware/v3/jwks"
	"github.com/auth0/go-jwt-middleware/v3/validator"
)

// NewApp serves GET /private, which only accepts access tokens issued by
// issuerURL for the given audience.
func NewApp(issuerURL, audience string) (http.Handler, error) {
	issuer, err := url.Parse(issuerURL)
	if err != nil {
		return nil, err
	}
	// The provider fetches the signing keys from the issuer's JWKS.
	provider, err := jwks.NewCachingProvider(jwks.WithIssuerURL(issuer))
	if err != nil {
		return nil, err
	}
	jwtValidator, err := validator.New(
		validator.WithKeyFunc(provider.KeyFunc),
		validator.WithAlgorithm(validator.RS256),
		validator.WithIssuer(issuer.String()),
		validator.WithAudience(audience),
	)
	if err != nil {
		return nil, err
	}
	middleware, err := jwtmiddleware.New(jwtmiddleware.WithValidator(jwtValidator))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /private", middleware.CheckJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := jwtmiddleware.GetClaims[*validator.ValidatedClaims](r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"sub": claims.RegisteredClaims.Subject})
	})))
	return mux, nil
}
