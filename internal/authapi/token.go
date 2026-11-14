package authapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

// grantPasswordlessOTP is Auth0's passwordless OTP grant type.
const grantPasswordlessOTP = "http://auth0.com/oauth/grant-type/passwordless/otp" //nolint:gosec // grant-type URI, not a credential

// TokenHandler handles OAuth token requests.
type TokenHandler struct {
	Keys            *jwks.KeySet
	Issuer          string
	DefaultAudience string
	Log             zerolog.Logger
	Claims          *claims.Store
	// ClaimMappings may be nil. When a request-parameter → claim-name
	// mapping is registered (via /admin0/claims/mappings), the value of that
	// body parameter is projected into every minted access token, taking
	// precedence over the global claims store. Empty store = feature off.
	ClaimMappings *claims.MappingStore
	Permissions   *permissions.Store
	// ManagementScopes is the default scope of a client_credentials token for
	// the Management API audience (see clientCredentialsScope).
	ManagementScopes []string
	// PKCE may be nil. When set and the authorization_code grant supplies a
	// code that was stashed at /authorize with a code_challenge, the matching
	// code_verifier is required and verified.
	PKCE *pkce.Store
	// MFA may be nil. When set and IsRequired() returns true, the password
	// and password-realm grants return 403 mfa_required + mfa_token instead
	// of minting; the three Auth0 mfa-* grants then trade the mfa_token plus
	// a fixed canned challenge for an access_token.
	MFA *mfa.Store
	// Sessions may be nil. When set, the profile of every id_token minted
	// is remembered per subject so /userinfo can return it, and each refresh
	// token's grant is remembered so refreshes keep the same user.
	Sessions *session.Store
	// Strict makes the authorization_code grant fail with 403 invalid_grant
	// when the code is unknown, used or expired, or when redirect_uri or
	// client_id differ from the /authorize request. It also omits
	// refresh_token when the scope lacks offline_access. Needs PKCE to be set.
	Strict bool
}

// errInvalidAuthCode is the error_description of a strict-mode code failure.
const errInvalidAuthCode = "Invalid authorization code"

// strictCodeError reports whether strict mode must reject the code exchange.
func strictCodeError(entry pkce.Entry, found bool, req *tokenRequest) bool {
	if !found {
		return true
	}
	if entry.Redirect != "" && req.RedirectURI != entry.Redirect {
		return true
	}
	// Compare clients exactly: a code issued without a client_id (allowed for
	// response_type=code) must not be redeemable by any client.
	return req.ClientID != entry.ClientID
}

// rememberProfile records the profile claims of an id_token's extra for
// /userinfo. Call it only after the id_token minted successfully.
func (h *TokenHandler) rememberProfile(subject string, extra map[string]any) {
	if h.Sessions != nil {
		h.Sessions.RememberProfile(subject, profileOf(extra))
	}
}

// issueRefreshToken returns a new refresh token. When Sessions is set it also
// remembers the grant so the refresh_token grant can return the same user. Call
// it only after the mints succeeded.
func (h *TokenHandler) issueRefreshToken(g session.Grant, idExtra map[string]any) string {
	token := uuid.NewString()
	if h.Sessions != nil {
		g.Profile = profileOf(idExtra)
		h.Sessions.RememberRefresh(token, g)
	}
	return token
}

// offlineRefreshToken is issueRefreshToken for the non-refresh grants. In
// strict mode it returns "" (and remembers nothing) unless the grant's scope
// includes offline_access.
func (h *TokenHandler) offlineRefreshToken(g session.Grant, idExtra map[string]any) string {
	if h.Strict && !slices.Contains(strings.Fields(g.Scope), "offline_access") {
		return ""
	}
	return h.issueRefreshToken(g, idExtra)
}

// mfaChallengeResponse is the body returned alongside the 403 mfa_required.
type mfaChallengeResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	MFAToken         string `json:"mfa_token"`
}

// requireMFA returns true (and writes the 403 challenge) if the MFA store
// reports MFA enforcement is on. Caller must abort token minting when true.
func (h *TokenHandler) requireMFA(w http.ResponseWriter, r *http.Request, ctx mfa.Context) bool {
	if h.MFA == nil || !h.MFA.IsRequired() {
		return false
	}
	tok := h.MFA.Issue(ctx)
	render.Status(r, http.StatusForbidden)
	render.JSON(w, r, mfaChallengeResponse{
		Error:            "mfa_required",
		ErrorDescription: "Multifactor authentication required",
		MFAToken:         tok,
	})
	return true
}

// augmentExtra layers per-audience permissions, per-process custom claims,
// and mapped request parameters onto the Extra map passed to jwks.Mint.
// Custom claims overwrite anything the grant set (gty, azp, even
// permissions); mapped request parameters take final precedence, so a
// single token request can carry a differently-scoped claim than the
// global claims store holds — without racing /admin0/claims between mints.
func (h *TokenHandler) augmentExtra(extra map[string]any, audience string, params map[string]any) map[string]any {
	if extra == nil {
		extra = make(map[string]any)
	}
	if h.Permissions != nil {
		if perms := h.Permissions.Get(audience); len(perms) > 0 {
			extra["permissions"] = perms
		}
	}
	if h.Claims != nil {
		h.Claims.MergeInto(extra)
	}
	if h.ClaimMappings != nil {
		h.ClaimMappings.Project(params, extra)
	}
	return extra
}

func (h *TokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenRequest(r)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.GrantType == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing grant_type")
		return
	}

	aud := h.resolveAudience(req.Audience)

	switch req.GrantType {
	case "client_credentials":
		h.respondClientCredentials(w, r, req, aud)
	case "password":
		h.respondPassword(w, r, req, aud)
	case "http://auth0.com/oauth/grant-type/password-realm":
		h.respondPasswordRealm(w, r, req, aud)
	case grantPasswordlessOTP:
		h.respondPasswordless(w, r, req, aud)
	case "refresh_token":
		h.respondRefreshToken(w, r, req, aud)
	case "authorization_code":
		h.respondAuthorizationCode(w, r, req, aud)
	case "http://auth0.com/oauth/grant-type/mfa-otp":
		h.respondMFAOTP(w, r, req)
	case "http://auth0.com/oauth/grant-type/mfa-oob":
		h.respondMFAOOB(w, r, req)
	case "http://auth0.com/oauth/grant-type/mfa-recovery-code":
		h.respondMFARecoveryCode(w, r, req)
	default:
		httperr.WriteAuth(w, http.StatusBadRequest, "unsupported_grant_type",
			"grant_type "+req.GrantType+" is not supported")
	}
}

// respondPasswordRealm handles Auth0's password-realm grant, which is the
// password grant plus a `realm` parameter selecting which connection to
// authenticate against (e.g. "Username-Password-Authentication" vs an
// enterprise connection). Used by Auth0 Native SDKs.
func (h *TokenHandler) respondPasswordRealm(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	if req.Realm == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing realm")
		return
	}
	subject := req.Username
	if subject == "" {
		subject = "auth0|" + uuid.NewString()
	}
	if h.requireMFA(w, r, mfa.Context{
		ClientID: req.ClientID, Audience: aud, Scope: req.Scope, Subject: subject, Realm: req.Realm,
	}) {
		return
	}
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra: h.augmentExtra(map[string]any{
			"gty":                     "password-realm",
			"azp":                     req.ClientID,
			"connection":              req.Realm,
			"https://auth0.com/realm": req.Realm,
		}, aud, req.Params),
	}, req.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	authTime := h.Keys.Cfg().Now()
	idExtra := fillProfile(map[string]any{
		"email":          subject,
		"email_verified": true,
		"name":           subject,
		"connection":     req.Realm,
		"azp":            req.ClientID,
		"auth_time":      authTime.Unix(),
	}, subject)
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
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: h.offlineRefreshToken(session.Grant{Subject: subject, ClientID: req.ClientID, Audience: aud, Scope: req.Scope, Realm: req.Realm, AuthTime: authTime}, idExtra),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        req.Scope,
	})
}

// respondPasswordless handles Auth0's passwordless OTP grant: the user proves
// an email address or phone number with the fixed mock OTP and gets tokens for
// a stable user derived from that identifier.
func (h *TokenHandler) respondPasswordless(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	for _, p := range []struct{ name, value string }{
		{"client_id", req.ClientID}, {"username", strings.TrimSpace(req.Username)}, {"otp", req.OTP}, {"realm", req.Realm},
	} {
		if p.value == "" {
			httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "Missing required parameter: "+p.name)
			return
		}
	}
	if req.Realm != "email" && req.Realm != "sms" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", `realm must be "email" or "sms"`)
		return
	}
	if req.OTP != acceptedPasswordlessOTP {
		desc := "Wrong email or verification code."
		if req.Realm == "sms" {
			desc = "Wrong phone number or verification code."
		}
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", desc)
		return
	}
	subject, profile := passwordlessUser(req.Realm, req.Username)
	if h.requireMFA(w, r, mfa.Context{
		ClientID: req.ClientID, Audience: aud, Scope: req.Scope, Subject: subject, Realm: req.Realm, Profile: profile,
	}) {
		return
	}
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra: h.augmentExtra(map[string]any{
			"gty":        "passwordless",
			"azp":        req.ClientID,
			"connection": req.Realm,
		}, aud, req.Params),
	}, req.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	authTime := h.Keys.Cfg().Now()
	idExtra := maps.Clone(profile)
	idExtra["azp"] = req.ClientID
	idExtra["auth_time"] = authTime.Unix()
	idExtra["connection"] = req.Realm
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
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: h.offlineRefreshToken(session.Grant{Subject: subject, ClientID: req.ClientID, Audience: aud, Scope: req.Scope, Realm: req.Realm, AuthTime: authTime}, idExtra),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        req.Scope,
	})
}

// parseTokenRequest accepts either application/json or
// application/x-www-form-urlencoded. Client credentials may also arrive via
// HTTP Basic (client_secret_basic); body values win over the header.
func parseTokenRequest(r *http.Request) (*tokenRequest, error) {
	req, err := parseTokenBody(r)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(r, req)
	return req, nil
}

// applyBasicAuth fills an empty ClientID/ClientSecret (and their Params
// entries, as a body-sent value would have) from a well-formed
// `Authorization: Basic` header. A malformed header is ignored.
func applyBasicAuth(r *http.Request, req *tokenRequest) {
	scheme, cred, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cred))
	if err != nil {
		return
	}
	id, secret, ok := strings.Cut(string(raw), ":")
	if !ok {
		return
	}
	// RFC 6749 §2.3.1: both halves are form-urlencoded before base64.
	unescape := func(v string) string {
		if u, err := url.QueryUnescape(v); err == nil {
			return u
		}
		return v
	}
	fill := func(field *string, key, v string) {
		if *field != "" || v == "" {
			return
		}
		*field = v
		if req.Params == nil {
			req.Params = map[string]any{}
		}
		if cur, present := req.Params[key]; !present || cur == "" || cur == nil {
			req.Params[key] = v
		}
	}
	fill(&req.ClientID, "client_id", unescape(id))
	fill(&req.ClientSecret, "client_secret", unescape(secret))
}

// parseTokenBody reads the token request from the JSON or form body.
func parseTokenBody(r *http.Request) (*tokenRequest, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		var req tokenRequest
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			// Second decode captures every raw key (including ones outside
			// the fixed field set) so claim mappings can project them.
			if err := json.Unmarshal(body, &req.Params); err != nil {
				return nil, err
			}
		}
		return &req, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	if r.PostForm == nil {
		return nil, errors.New("no form body")
	}
	params := make(map[string]any, len(r.PostForm))
	for k, vs := range r.PostForm {
		// RFC 6749 §3.2: parameters sent without a value MUST be treated
		// as if they were omitted, so `&resource=` must not project (or
		// clobber a global claim default). JSON bodies differ on purpose:
		// an explicit "" or null there is a present value and projects.
		if len(vs) > 0 && vs[0] != "" {
			params[k] = vs[0]
		}
	}
	return &tokenRequest{
		Params:       params,
		GrantType:    r.PostForm.Get("grant_type"),
		ClientID:     r.PostForm.Get("client_id"),
		ClientSecret: r.PostForm.Get("client_secret"),
		Audience:     r.PostForm.Get("audience"),
		Scope:        r.PostForm.Get("scope"),
		Username:     r.PostForm.Get("username"),
		Password:     r.PostForm.Get("password"),
		RefreshToken: r.PostForm.Get("refresh_token"),
		Code:         r.PostForm.Get("code"),
		RedirectURI:  r.PostForm.Get("redirect_uri"),
		CodeVerifier: r.PostForm.Get("code_verifier"),
		Realm:        r.PostForm.Get("realm"),
		MFAToken:     r.PostForm.Get("mfa_token"),
		OTP:          r.PostForm.Get("otp"),
		OOBCode:      r.PostForm.Get("oob_code"),
		BindingCode:  r.PostForm.Get("binding_code"),
		RecoveryCode: r.PostForm.Get("recovery_code"),
	}, nil
}

// resolveAudience falls back to the default audience when the requested one is
// blank, and normalises a still-blank result to "".
func (h *TokenHandler) resolveAudience(requested string) string {
	aud := requested
	if strings.TrimSpace(aud) == "" {
		aud = h.DefaultAudience
	}
	if strings.TrimSpace(aud) == "" {
		return ""
	}
	return aud
}

// mintAccess mints an access token. With no audience it returns an opaque
// token, as an Auth0 tenant without a default audience does: 32 random bytes,
// remembered in the session store so only /userinfo accepts it. Otherwise it
// returns a signed JWT.
func (h *TokenHandler) mintAccess(opts jwks.MintOpts, clientID string) (string, error) {
	if len(opts.Audience) > 0 && strings.TrimSpace(opts.Audience[0]) != "" {
		return h.Keys.Mint(opts)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	if h.Sessions != nil {
		h.Sessions.RememberAccess(token, session.Access{
			Subject: opts.Subject, ClientID: clientID, Scope: opts.Scope,
			Issuer:    opts.Issuer,
			ExpiresAt: h.Keys.Cfg().Now().Add(opts.TTL),
		})
	}
	return token, nil
}

func (h *TokenHandler) respondClientCredentials(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	if aud == "" {
		httperr.WriteAuth(w, http.StatusForbidden, "access_denied", "No audience parameter was provided, and no default audience has been configured")
		return
	}
	scope := h.clientCredentialsScope(aud, req.Scope)
	access, err := h.Keys.Mint(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  req.ClientID + "@clients",
		Audience: []string{aud},
		Scope:    scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "client-credentials", "azp": req.ClientID}, aud, req.Params),
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	render.JSON(w, r, tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:       scope,
	})
}

// clientCredentialsScope returns the scope for a client_credentials token: the
// requested scope when it is non-blank, otherwise the audience's registered
// permissions joined by spaces (what Auth0 reports as the client's granted
// scopes). For the Management API audience with nothing registered it is
// every Management scope, so a default token can call any endpoint; an empty
// registration grants none. Empty otherwise, which omits the claim.
func (h *TokenHandler) clientCredentialsScope(audience, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return requested
	}
	if h.Permissions != nil {
		if perms, ok := h.Permissions.Lookup(audience); ok {
			return strings.Join(perms, " ")
		}
	}
	if isManagementAudience(audience) {
		return strings.Join(h.ManagementScopes, " ")
	}
	return ""
}

// isManagementAudience reports whether audience is a Management API audience:
// an absolute URL whose path is /api/v2/ (or /api/v2), on any scheme, host or
// port. Auth0's is always https://<domain>/api/v2/; the mock's host varies.
func isManagementAudience(audience string) bool {
	u, err := url.Parse(audience)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.Path == "/api/v2/" || u.Path == "/api/v2"
}

func (h *TokenHandler) respondPassword(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	subject := req.Username
	if subject == "" {
		subject = "auth0|" + uuid.NewString()
	}
	if h.requireMFA(w, r, mfa.Context{
		ClientID: req.ClientID, Audience: aud, Scope: req.Scope, Subject: subject,
	}) {
		return
	}
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  subject,
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "password", "azp": req.ClientID}, aud, req.Params),
	}, req.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	authTime := h.Keys.Cfg().Now()
	idExtra := fillProfile(map[string]any{
		"email":          subject,
		"email_verified": true,
		"name":           subject,
		"azp":            req.ClientID,
		"auth_time":      authTime.Unix(),
	}, subject)
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
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: h.offlineRefreshToken(session.Grant{Subject: subject, ClientID: req.ClientID, Audience: aud, Scope: req.Scope, AuthTime: authTime}, idExtra),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        req.Scope,
	})
}

func (h *TokenHandler) respondRefreshToken(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	var grant session.Grant
	var known bool
	if h.Sessions != nil {
		grant, known = h.Sessions.Refresh(req.RefreshToken)
	}
	if !known {
		h.respondUnknownRefreshToken(w, r, req, aud)
		return
	}
	// Request values win; an omitted audience/scope falls back to the stored
	// one when non-empty (not the defaulted aud, which would always shadow it);
	// otherwise the already-defaulted aud stands.
	if strings.TrimSpace(req.Audience) != "" {
		aud = req.Audience
	} else if grant.Audience != "" {
		aud = grant.Audience
	}
	scope := req.Scope
	if scope == "" {
		scope = grant.Scope
	}
	clientID := req.ClientID
	if clientID == "" {
		clientID = grant.ClientID
	}
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  grant.Subject,
		Audience: []string{aud},
		Scope:    scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "refresh-token", "azp": clientID}, aud, req.Params),
	}, clientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	idExtra := maps.Clone(grant.Profile)
	if idExtra == nil {
		idExtra = deriveProfile(grant.Subject)
	}
	idExtra["azp"] = clientID
	idExtra["auth_time"] = grant.AuthTime.Unix()
	if grant.Realm != "" {
		idExtra["connection"] = grant.Realm
	}
	id, err := h.Keys.Mint(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  grant.Subject,
		Audience: []string{clientID},
		TTL:      h.Keys.Cfg().IDTokenTTL,
		Extra:    idExtra,
	})
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	h.rememberProfile(grant.Subject, idExtra)
	grant.Audience, grant.Scope, grant.ClientID = aud, scope, clientID
	render.JSON(w, r, tokenResponse{
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: h.issueRefreshToken(grant, idExtra),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        scope,
	})
}

// respondUnknownRefreshToken keeps the lenient behaviour for refresh tokens
// the mock did not issue: an access_token for "<client_id>@refresh", carrying
// the requested scope (there is no stored grant to fall back to).
func (h *TokenHandler) respondUnknownRefreshToken(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  req.ClientID + "@refresh",
		Audience: []string{aud},
		Scope:    req.Scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "refresh-token", "azp": req.ClientID}, aud, req.Params),
	}, req.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	render.JSON(w, r, tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:       req.Scope,
	})
}

// pkceFailure applies the PKCE matrix documented in respondAuthorizationCode
// and returns the error_description of a failure, or "" when the exchange may
// proceed.
func pkceFailure(entry pkce.Entry, hasEntry bool, verifier string) string {
	hasChallenge := hasEntry && entry.Challenge != ""
	if !hasChallenge && verifier == "" {
		return ""
	}
	if !hasChallenge {
		return "PKCE verification failed: no stored challenge for this authorization_code (expired or never issued)"
	}
	if err := entry.Verify(verifier); err != nil {
		return "PKCE verification failed: " + err.Error()
	}
	return ""
}

func (h *TokenHandler) respondAuthorizationCode(w http.ResponseWriter, r *http.Request, req *tokenRequest, aud string) {
	// PKCE verification. The matrix ("stored" means an entry with a challenge):
	//   - challenge stored + verifier supplied → must match (existing-correct path)
	//   - challenge stored + no verifier       → entry.Verify("") returns
	//     "missing code_verifier" → 400
	//   - no challenge stored + verifier supplied → client thinks PKCE is in
	//     play but the server has no challenge (expired, swept, never sent at
	//     /authorize, or the code was issued without PKCE). Failing closed
	//     here closes a real bypass: a stolen `code` from a non-PKCE flow
	//     would otherwise be redeemable.
	//   - no challenge stored + no verifier       → legitimate non-PKCE flow → pass.
	if h.Strict && req.Code == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "Missing required parameter: code")
		return
	}
	var entry pkce.Entry
	var hasEntry bool
	if h.PKCE != nil && req.Code != "" {
		entry, hasEntry = h.PKCE.Consume(req.Code)
		if h.Strict && strictCodeError(entry, hasEntry, req) {
			httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", errInvalidAuthCode)
			return
		}
		if msg := pkceFailure(entry, hasEntry, req.CodeVerifier); msg != "" {
			httperr.WriteAuth(w, http.StatusBadRequest, "invalid_grant", msg)
			return
		}
	}
	subject := DefaultSubject
	if hasEntry && entry.LoginHint != "" {
		subject = entry.LoginHint
	}
	aud, scope := codeAudienceScope(entry, aud, req.Scope, h.Strict && hasEntry)
	access, err := h.mintAccess(jwks.MintOpts{
		Issuer:   h.Keys.IssuerFor(r),
		Subject:  subject,
		Audience: []string{aud},
		Scope:    scope,
		TTL:      h.Keys.Cfg().AccessTokenTTL,
		Extra:    h.augmentExtra(map[string]any{"gty": "authorization-code", "azp": req.ClientID}, aud, req.Params),
	}, req.ClientID)
	if err != nil {
		httperr.WriteAuth(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	idExtra := deriveProfile(subject)
	idExtra["azp"] = req.ClientID
	authTime := h.Keys.Cfg().Now()
	if hasEntry {
		authTime = entry.AuthTime
		if entry.Nonce != "" {
			idExtra["nonce"] = entry.Nonce
		}
	}
	idExtra["auth_time"] = authTime.Unix()
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
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: h.offlineRefreshToken(session.Grant{Subject: subject, ClientID: req.ClientID, Audience: aud, Scope: scope, AuthTime: authTime}, idExtra),
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Keys.Cfg().AccessTokenTTL.Seconds()),
		Scope:        scope,
	})
}

// codeAudienceScope returns the audience and scope for a code exchange. The
// code carries the /authorize audience and scope, which win over the token
// request's, as on Auth0. A zero entry (no stored code) keeps the request's.
// With storedScope set (strict mode) the scope is the stored one even when
// empty, and the request's scope is ignored.
func codeAudienceScope(entry pkce.Entry, aud, scope string, storedScope bool) (string, string) {
	if entry.Audience != "" {
		aud = entry.Audience
	}
	if entry.Scope != "" || storedScope {
		scope = entry.Scope
	}
	return aud, scope
}
