package authapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/session"
)

const (
	// GrantTokenExchange is the RFC 8693 token-exchange grant type.
	grantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // grant-type URI, not a credential
	// IssuedTokenTypeAccess is the issued_token_type of every exchange.
	issuedTokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token" //nolint:gosec // token-type URI, not a credential
)

// validSubjectTokenType reports whether t is a https:// or urn: URI outside
// the reserved auth0, okta and ietf namespaces, as Auth0 requires.
func validSubjectTokenType(t string) bool {
	lower := strings.ToLower(t)
	switch {
	case strings.HasPrefix(lower, "https://"):
		host := strings.TrimPrefix(lower, "https://")
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		if host == "" {
			return false
		}
		for _, r := range []string{"auth0.com", "okta.com"} {
			if host == r || strings.HasSuffix(host, "."+r) {
				return false
			}
		}
		return true
	case strings.HasPrefix(lower, "urn:"):
		rest := t[len("urn:"):]
		if rest == "" {
			return false
		}
		ns, _, _ := strings.Cut(rest, ":")
		switch strings.ToLower(ns) {
		case "ietf", "auth0", "okta":
			return false
		}
		return true
	}
	return false
}

// subjectFromToken reads the user out of a subject token. A JWT-shaped token
// has its payload decoded (signature not checked): sub is the user and the
// profile claims are kept. Anything else is a literal user id. Ok is false
// only for a JWT-shaped token that is malformed, has no sub, or is expired.
func subjectFromToken(tok string, now time.Time) (sub string, profile map[string]any, ok bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || !isJSONSegment(parts[0]) {
		return tok, nil, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", nil, false
	}
	sub, _ = payload["sub"].(string)
	if sub == "" {
		return "", nil, false
	}
	if exp, has := payload["exp"].(float64); has && !now.Before(time.Unix(int64(exp), 0)) {
		return "", nil, false
	}
	return sub, profileOf(payload), true
}

// isJSONSegment reports whether a base64url segment decodes to a JSON object.
func isJSONSegment(seg string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return false
	}
	var m map[string]any
	return json.Unmarshal(raw, &m) == nil
}

// respondTokenExchange handles the RFC 8693 token-exchange grant: the subject
// token names the user (its signature is not verified) and the caller gets
// tokens for that user, optionally scoped to an organization.
func (h *TokenHandler) respondTokenExchange(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	switch {
	case req.SubjectToken == "":
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "Missing required parameter: subject_token")
		return
	case req.SubjectTokenType == "":
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "Missing required parameter: subject_token_type")
		return
	case !validSubjectTokenType(req.SubjectTokenType):
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request",
			"subject_token_type must be a https:// or urn: URI outside the reserved auth0, okta and ietf namespaces")
		return
	}
	authTime := h.Keys.Cfg().Now()
	subject, profile, ok := subjectFromToken(req.SubjectToken, authTime)
	if !ok {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "Invalid subject token")
		return
	}
	org := resolveOrganization(req.Organization)
	base := map[string]any{"gty": "token-exchange", "azp": req.ClientID}
	org.addClaims(base)
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(base, aud, req.Params),
	}, req.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	idExtra := map[string]any{"azp": req.ClientID, "auth_time": authTime.Unix()}
	for k, v := range profile {
		idExtra[k] = v
	}
	fillProfile(idExtra, subject)
	org.addClaims(idExtra)
	id, err := h.Keys.Mint(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  subject,
		Audience: []string{req.ClientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra:    idExtra,
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	h.rememberProfile(subject, idExtra)
	render.JSON(w, r, tokenResponse{
		AccessToken:     access,
		IDToken:         id,
		RefreshToken:    h.offlineRefreshToken(session.Grant{Subject: subject, ClientID: req.ClientID, Audience: aud, Scope: req.Scope, AuthTime: authTime, OrgID: org.ID, OrgName: org.Name}, idExtra),
		TokenType:       "Bearer",
		ExpiresIn:       int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:           req.Scope,
		IssuedTokenType: issuedTokenTypeAccess,
	})
}
