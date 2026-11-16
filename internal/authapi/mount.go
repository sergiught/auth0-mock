// Package authapi mounts hand-coded Auth0 Authentication API endpoints onto
// chi. Unlike the Mgmt API, these endpoints are functional — they mint
// real RS256 JWTs and respond with valid OIDC discovery / JWKS payloads.
package authapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

// Deps is the parameter object for Mount.
type Deps struct {
	Router          chi.Router
	Keys            *jwks.KeySet
	Issuer          string
	DefaultAudience string
	// ManagementScopes is every scope the Management API spec lists. A
	// client_credentials token for the Management API audience gets all of
	// them when no narrower scope is requested or registered.
	ManagementScopes []string
	Log              zerolog.Logger
	Claims           *claims.Store
	ClaimMappings    *claims.MappingStore
	Permissions      *permissions.Store
	PKCE             *pkce.Store
	MFA              *mfa.Store
	// Sessions may be nil (no remembered profiles). With
	// TokenEndpointStrict, Mount creates one when it is nil.
	Sessions                     *session.Store
	LogoutAllowedURLs            []string
	AuthorizeAllowedRedirectURIs []string
	// AuthorizeErrors may be nil (no /authorize failure rules).
	AuthorizeErrors *authorizeerr.Store
	// TokenEndpointStrict turns on the authorization-code checks at
	// /oauth/token, drops refresh_token without offline_access, rotates
	// refresh tokens and makes /oauth/revoke end the chain (see
	// TokenHandler.Strict).
	TokenEndpointStrict bool

	// LoginPage sends logged-out /authorize requests to /u/login. Like
	// TokenEndpointStrict it makes Mount create Sessions when nil.
	LoginPage bool
}

// Mount registers all Auth API endpoints on d.Router.
func Mount(d Deps) {
	if (d.TokenEndpointStrict || d.LoginPage) && d.Sessions == nil {
		// Strict refresh tokens are only valid once stored, so strict mode
		// needs a session store to issue usable ones.
		d.Sessions = session.NewStore()
	}
	// Device entries live in the shared store when there is one; otherwise in a
	// store only the device endpoints see.
	devices := d.Sessions
	if devices == nil {
		devices = session.NewStore()
	}
	tok := &TokenHandler{
		Keys:             d.Keys,
		Issuer:           d.Issuer,
		DefaultAudience:  d.DefaultAudience,
		ManagementScopes: d.ManagementScopes,
		Log:              d.Log,
		Claims:           d.Claims,
		ClaimMappings:    d.ClaimMappings,
		Permissions:      d.Permissions,
		PKCE:             d.PKCE,
		MFA:              d.MFA,
		Sessions:         d.Sessions,
		Devices:          devices,
		Strict:           d.TokenEndpointStrict,
	}
	d.Router.Method(http.MethodPost, "/oauth/token", tok)
	d.Router.Method(http.MethodPost, "/oauth/device/code", &DeviceCodeHandler{Token: tok, Devices: devices, LoginPage: d.LoginPage})
	d.Router.Method(http.MethodPost, "/mfa/challenge", &MFAChallengeHandler{MFA: d.MFA})
	d.Router.Method(http.MethodPost, "/mfa/associate", &MFAAssociateHandler{MFA: d.MFA})
	authenticators := &MFAAuthenticatorsHandler{MFA: d.MFA, Keys: d.Keys}
	d.Router.Get("/mfa/authenticators", authenticators.List)
	d.Router.Delete("/mfa/authenticators/{id}", authenticators.Delete)
	authorizeHandler := &AuthorizeHandler{
		PKCE:                d.PKCE,
		AllowedRedirectURIs: d.AuthorizeAllowedRedirectURIs,
		Errors:              d.AuthorizeErrors,
		Token:               tok,
		Sessions:            d.Sessions,
		LoginPage:           d.LoginPage,
	}
	d.Router.Method(http.MethodGet, "/authorize", authorizeHandler)
	if d.LoginPage {
		loginPage := &LoginPageHandler{Authorize: authorizeHandler}
		d.Router.Method(http.MethodGet, "/u/login", loginPage)
		d.Router.Method(http.MethodPost, "/u/login", loginPage)
		activate := &ActivateHandler{Authorize: authorizeHandler, Devices: devices}
		d.Router.Method(http.MethodGet, "/activate", activate)
		d.Router.Method(http.MethodPost, "/activate", activate)
	}
	userInfo := &UserInfoHandler{Keys: d.Keys, Sessions: d.Sessions}
	d.Router.Method(http.MethodGet, "/userinfo", userInfo)
	d.Router.Method(http.MethodPost, "/userinfo", userInfo)
	d.Router.Method(http.MethodGet, "/.well-known/openid-configuration", NewDiscoveryHandler(d.Issuer))
	d.Router.Method(http.MethodGet, "/v2/logout", &LogoutHandler{AllowedReturnURLs: d.LogoutAllowedURLs, Sessions: d.Sessions})
	oidcLogout := &OIDCLogoutHandler{Keys: d.Keys, AllowedReturnURLs: d.LogoutAllowedURLs, Sessions: d.Sessions}
	d.Router.Method(http.MethodGet, "/oidc/logout", oidcLogout)
	d.Router.Method(http.MethodPost, "/oidc/logout", oidcLogout)
	d.Router.Method(http.MethodPost, "/oauth/revoke", &RevokeHandler{Strict: d.TokenEndpointStrict, Sessions: d.Sessions})
	d.Router.Method(http.MethodPost, "/dbconnections/signup", &DBConnectionsSignupHandler{})
	d.Router.Method(http.MethodPost, "/dbconnections/change_password", &DBConnectionsChangePasswordHandler{})
	d.Router.Method(http.MethodPost, "/passwordless/start", &PasswordlessStartHandler{})
	d.Router.Method(http.MethodPost, "/passwordless/verify", &PasswordlessVerifyHandler{Token: tok})
}
