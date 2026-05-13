// Package authapi mounts hand-coded Auth0 Authentication API endpoints onto
// httprouter. Unlike the Mgmt API, these endpoints are functional — they mint
// real RS256 JWTs and respond with valid OIDC discovery / JWKS payloads.
package authapi

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/jwks"
)

// Deps is the parameter object for Mount.
type Deps struct {
	Router          *httprouter.Router
	Keys            *jwks.KeySet
	Issuer          string
	DefaultAudience string
	Log             zerolog.Logger
}

// Mount registers all Auth API endpoints on d.Router.
func Mount(d Deps) {
	t := newTokenHandler(d)
	d.Router.HandlerFunc(http.MethodPost, "/oauth/token", t.handle)

	d.Router.HandlerFunc(http.MethodGet, "/authorize", authorize(d))
	d.Router.HandlerFunc(http.MethodGet, "/userinfo", userinfo(d))
	d.Router.HandlerFunc(http.MethodGet, "/.well-known/openid-configuration", discovery(d))
	d.Router.HandlerFunc(http.MethodGet, "/v2/logout", logout(d))
	d.Router.HandlerFunc(http.MethodPost, "/oauth/revoke", revoke(d))
	d.Router.HandlerFunc(http.MethodPost, "/dbconnections/signup", dbconnectionsSignup(d))
	d.Router.HandlerFunc(http.MethodPost, "/dbconnections/change_password", dbconnectionsChangePassword(d))
}
