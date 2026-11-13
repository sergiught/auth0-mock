package authapi

import (
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/bearer"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/session"
)

// UserInfoHandler returns claims for the authenticated user.
type UserInfoHandler struct {
	Keys *jwks.KeySet
	// Sessions may be nil. When set, the profile remembered for the token's
	// subject is added beneath the token's own claims.
	Sessions *session.Store
}

// bodyForm parses a form-encoded request body, ignoring the URL query.
// Other content types yield no values.
func bodyForm(r *http.Request) (url.Values, error) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/x-www-form-urlencoded" {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body) // Capped by the global MAX_REQUEST_BODY_BYTES middleware.
	if err != nil {
		return nil, err
	}
	return url.ParseQuery(string(body))
}

// userinfoToken returns the access token from the Authorization header or,
// on POST, from an access_token field in a form-encoded body (RFC 6750 2.2).
// Only the body is parsed, so a bad query string cannot fail the request (GET
// ignores it too). A non-empty problem is the description for a 400
// invalid_request.
func userinfoToken(r *http.Request) (token string, ok bool, problem string) {
	token, ok = bearer.Token(r.Header.Get("Authorization"))
	if r.Method != http.MethodPost {
		return token, ok, ""
	}
	form, err := bodyForm(r)
	if err != nil {
		return "", false, "malformed request body"
	}
	// RFC 6750 3.1: a repeated parameter is invalid_request.
	if len(form["access_token"]) > 1 {
		return "", false, "access_token must be sent only once"
	}
	if formToken := form.Get("access_token"); formToken != "" {
		if ok {
			return "", false, "access token must be sent using only one method"
		}
		return formToken, true, ""
	}
	return token, ok, ""
}

func (h *UserInfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, ok, problem := userinfoToken(r)
	if problem != "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", problem)
		return
	}
	if !ok {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
		return
	}
	// /userinfo accepts any audience (it's about who the user is, not
	// what API they're authorised to call), so no RequireAudience here.
	// Matches real Auth0's /userinfo behaviour.
	claims, err := h.Keys.Verify(token, jwks.VerifyOpts{Issuer: h.Keys.IssuerFor(r)})
	if err != nil {
		if h.Sessions != nil {
			if a, found := h.Sessions.Access(token); found && a.Issuer == h.Keys.IssuerFor(r) && h.Keys.Cfg().Now().Before(a.ExpiresAt) {
				out := map[string]any{}
				if profile, ok := h.Sessions.Profile(a.Subject); ok {
					maps.Copy(out, profile)
				}
				out["sub"] = a.Subject
				render.JSON(w, r, out)
				return
			}
		}
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", err.Error())
		return
	}
	out := make(map[string]any, len(claims.Extra)+1)
	out["sub"] = claims.Subject
	// Machine tokens have no user, even when "<client_id>@clients" happens
	// to match a remembered subject.
	if h.Sessions != nil && claims.Extra["gty"] != "client-credentials" {
		if profile, ok := h.Sessions.Profile(claims.Subject); ok {
			maps.Copy(out, profile)
		}
	}
	maps.Copy(out, claims.Extra)
	render.JSON(w, r, out)
}
