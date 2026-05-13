// Package authapi mounts hand-coded Auth0 Authentication API endpoints onto
// chi. Unlike the Mgmt API, these endpoints are functional — they mint
// real RS256 JWTs and respond with valid OIDC discovery / JWKS payloads.
package authapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
)

// Deps is the parameter object for Mount.
type Deps struct {
	Router          chi.Router
	Keys            *jwks.KeySet
	Issuer          string
	DefaultAudience string
	Log             zerolog.Logger
	Claims          *claims.Store
	Permissions     *permissions.Store
	PKCE            *pkce.Store
}

// Mount registers all Auth API endpoints on d.Router.
func Mount(d Deps) {
	d.Router.Method(http.MethodPost, "/oauth/token", &TokenHandler{
		Keys:            d.Keys,
		Issuer:          d.Issuer,
		DefaultAudience: d.DefaultAudience,
		Log:             d.Log,
		Claims:          d.Claims,
		Permissions:     d.Permissions,
		PKCE:            d.PKCE,
	})
	d.Router.Method(http.MethodGet, "/authorize", &AuthorizeHandler{PKCE: d.PKCE})
	d.Router.Method(http.MethodGet, "/userinfo", &UserInfoHandler{Keys: d.Keys})
	d.Router.Method(http.MethodGet, "/.well-known/openid-configuration", &DiscoveryHandler{Issuer: d.Issuer})
	d.Router.Method(http.MethodGet, "/v2/logout", &LogoutHandler{})
	d.Router.Method(http.MethodPost, "/oauth/revoke", &RevokeHandler{})
	d.Router.Method(http.MethodPost, "/dbconnections/signup", &DBConnectionsSignupHandler{})
	d.Router.Method(http.MethodPost, "/dbconnections/change_password", &DBConnectionsChangePasswordHandler{})
	d.Router.Method(http.MethodPost, "/passwordless/start", &PasswordlessStartHandler{})
	d.Router.Method(http.MethodPost, "/passwordless/verify", &PasswordlessVerifyHandler{
		Keys:            d.Keys,
		DefaultAudience: d.DefaultAudience,
		Claims:          d.Claims,
		Permissions:     d.Permissions,
	})
}
