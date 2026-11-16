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
	// The grant type of the RFC 8693 token exchange, grantTokenExchange.
	grantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // grant-type URI, not a credential
	// The issued_token_type of every exchange.
	issuedTokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token" //nolint:gosec // token-type URI, not a credential
)

// validSubjectTokenType reports whether t is a https:// or urn: URI outside
// the reserved auth0, okta and ietf namespaces, by Auth0's prefix rule.
func validSubjectTokenType(t string) bool {
	lower := strings.ToLower(t)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "urn:") {
		return false
	}
	for _, p := range []string{
		"http://auth0.com", "https://auth0.com", "http://okta.com", "https://okta.com",
		"urn:ietf", "urn:auth0", "urn:okta",
	} {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	return true
}

// subjectFromToken reads the user out of a subject token. A JWT-shaped token
// has its payload decoded (signature not checked): sub is the user and the
// profile claims are kept. Anything else is a literal user id. Ok is false
// only for a JWT-shaped token that is malformed, has no sub, or is expired.
func subjectFromToken(tok string, now time.Time) (sub string, profile map[string]any, ok bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" || !isJSONSegment(parts[0]) {
		return tok, nil, true
	}
	raw, err := decodeSegment(parts[1])
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
	if exp, has := payload["exp"].(float64); has && float64(now.Unix()) >= exp {
		return "", nil, false
	}
	return sub, profileOf(payload), true
}

// decodeSegment decodes a base64url segment, with or without padding.
func decodeSegment(seg string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(seg, "="))
}

// isJSONSegment reports whether a base64url segment decodes to a JSON object.
func isJSONSegment(seg string) bool {
	raw, err := decodeSegment(seg)
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
