package authapi

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

// AuthorizeHandler handles OIDC authorization requests.
type AuthorizeHandler struct {
	// PKCE may be nil; when set, /authorize will stash any code_challenge it
	// receives so the matching /oauth/token exchange can verify the
	// code_verifier.
	PKCE *pkce.Store
	// AllowedRedirectURIs is the allow-list of absolute redirect_uri values
	// that /authorize will 302 to. Mirrors Auth0's per-application
	// "Allowed Callback URLs" tenant setting. Same threat model as
	// LogoutHandler.AllowedReturnURLs but on the higher-value endpoint:
	// /authorize carries `code` / `access_token` in the URL, so an
	// unvalidated redirect_uri leaks them to attacker-controlled hosts.
	// Relative URIs are always permitted. Empty list = no enforcement
	// (test-friendly default).
	AllowedRedirectURIs []string
	// Errors holds the failure rules; nil means no rules.
	Errors *authorizeerr.Store
	// Token mints the access and id tokens for response types other than code; required for those types.
	Token *TokenHandler
	// Sessions holds the login sessions behind the auth0 cookie; nil means no
	// sessions, so prompt=none always answers login_required.
	Sessions *session.Store
}

// responseTypes is the parsed response_type set.
type responseTypes struct {
	code, token, idToken bool
}

// parseResponseTypes splits a space-separated response_type into a set.
// Order and duplicates are irrelevant; an empty value means "code". The second
// result is false when the value holds an unknown word.
func parseResponseTypes(raw string) (responseTypes, bool) {
	var rt responseTypes
	words := strings.Fields(raw)
	if len(words) == 0 {
		return responseTypes{code: true}, true
	}
	for _, w := range words {
		switch w {
		case "code":
			rt.code = true
		case "token":
			rt.token = true
		case "id_token":
			rt.idToken = true
		default:
			return responseTypes{}, false
		}
	}
	return rt, true
}

func (h *AuthorizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if redirect == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing redirect_uri")
		return
	}
	state := q.Get("state")
	responseType := q.Get("response_type")
	rt, validType := parseResponseTypes(responseType)
	// Anything other than exactly {code} defaults to fragment delivery.
	fragmentDefault := rt.token || rt.idToken

	u, ok := h.vetRedirect(w, redirect)
	if !ok {
		return
	}
	mode := resolveMode(q.Get("response_mode"), fragmentDefault)
	// Go html/template rewrites a form action with a non-http(s) scheme to
	// #ZgotmplZ, and web_message needs an origin to post to, so fall back to
	// the default mode for such redirect URIs.
	if (mode == modeFormPost || mode == modeWebMessage) && u.Scheme != "http" && u.Scheme != "https" {
		mode = resolveMode("", fragmentDefault)
	}

	// Reject unservable requests before anything is minted or recorded.
	if code, desc := validateResponseType(q, responseType, rt, validType); code != "" {
		deliver(w, u, mode, errorParams(code, desc, state))
		return
	}

	// A registered failure rule wins over a normal login. It runs after the
	// redirect_uri checks so an error is never sent to an unvetted URI, and
	// before any code is minted so a failed request leaves no PKCE state.
	if h.Errors != nil {
		if rule := h.Errors.Find(q); rule != nil {
			deliver(w, u, mode, errorParams(rule.Error, rule.ErrorDescription, state))
			return
		}
	}

	subject, authTime, reuse, ok := h.resolveLogin(r, q)
	if !ok {
		deliver(w, u, mode, errorParams("login_required", "Login required", state))
		return
	}

	params, ok := h.issue(w, u, mode, q, rt, state, subject, authTime)
	if !ok {
		return
	}
	if !reuse && h.Sessions != nil {
		h.startSession(w, r, subject, authTime)
	}

	deliver(w, u, mode, params)
}

// issue builds the success parameters: the code, any tokens, and state. It
// delivers the error itself and returns false when minting fails.
func (h *AuthorizeHandler) issue(w http.ResponseWriter, u *url.URL, mode string, q url.Values, rt responseTypes, state, subject string, authTime time.Time) (url.Values, bool) {
	params := url.Values{}
	var issuedCode string
	if rt.code {
		issuedCode = uuid.NewString()
		params.Set("code", issuedCode)
	}
	if rt.token || rt.idToken {
		if !h.mintTokens(w, u, mode, q, rt, issuedCode, state, subject, authTime, params) {
			return nil, false
		}
	}
	h.recordCode(q, issuedCode, subject, authTime)
	if state != "" {
		params.Set("state", state)
	}
	return params, true
}

// now is the mock clock: the key set's when tokens are minted, else wall time.
func (h *AuthorizeHandler) now() time.Time {
	if h.Token != nil {
		return h.Token.Keys.Cfg().Now()
	}
	return time.Now()
}

// resolveLogin decides who is logging in and when they authenticated.
// Reuse reports that the request's live session stays as is. Ok is false when
// prompt=none finds no live session, which is a login_required error.
func (h *AuthorizeHandler) resolveLogin(r *http.Request, q url.Values) (subject string, authTime time.Time, reuse, ok bool) {
	_, login, live := currentLogin(r, h.Sessions)
	prompts := strings.Fields(q.Get("prompt"))
	if slices.Contains(prompts, "none") {
		return login.Subject, login.AuthTime, true, live
	}
	if hint := q.Get("login_hint"); hint != "" {
		if live && hint == login.Subject && !slices.Contains(prompts, "login") {
			return login.Subject, login.AuthTime, true, true
		}
		return hint, h.now(), false, true
	}
	if live && !slices.Contains(prompts, "login") {
		return login.Subject, login.AuthTime, true, true
	}
	return DefaultSubject, h.now(), false, true
}

// startSession replaces the request's login session, if any, with a new one
// for subject and sets its cookie.
func (h *AuthorizeHandler) startSession(w http.ResponseWriter, r *http.Request, subject string, authTime time.Time) {
	if old, _, live := currentLogin(r, h.Sessions); live {
		h.Sessions.EndLogin(old)
	}
	setLoginCookie(w, r, h.Sessions.StartLogin(subject, authTime))
}

// vetRedirect parses redirect and applies the allow-list, writing the 400
// itself when the URI is unusable.
func (h *AuthorizeHandler) vetRedirect(w http.ResponseWriter, redirect string) (*url.URL, bool) {
	u, err := url.Parse(redirect)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "invalid redirect_uri")
		return nil, false
	}
	if !h.isRedirectAllowed(redirect) {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request",
			"redirect_uri is not in the configured allow-list (AUTHORIZE_ALLOWED_CALLBACKS); add it there to permit this redirect")
		return nil, false
	}
	return u, true
}

// validateResponseType returns the OAuth error code and description for a
// request that cannot be served, or empty strings: an unknown response_type,
// a code_challenge outside the RFC 7636 length range, or missing parameters
// for a token-issuing one. RFC 7636 section 4.1 requires the code_verifier
// (and therefore the plain code_challenge, or the pre-hash for S256) to be
// 43..128 characters. Rejecting out-of-range values at /authorize gives the
// client a real error instead of a code that silently fails to exchange
// later. It runs even when the PKCE store isn't wired so client-side bugs
// surface either way.
func validateResponseType(q url.Values, raw string, rt responseTypes, valid bool) (code, description string) {
	if !valid {
		return "unsupported_response_type", "Unsupported response type: " + raw
	}
	if challenge := q.Get("code_challenge"); challenge != "" {
		if n := len(challenge); n < 43 || n > 128 {
			return "invalid_request", fmt.Sprintf("code_challenge must be 43..128 characters (RFC 7636 section 4.1), got %d", n)
		}
	}
	if !rt.token && !rt.idToken {
		return "", ""
	}
	if q.Get("client_id") == "" {
		return "invalid_request", "Missing required parameter: client_id"
	}
	if rt.idToken && q.Get("nonce") == "" {
		return "invalid_request", "nonce is required when response_type includes id_token"
	}
	return "", ""
}

// mintTokens adds the access_token / id_token parameters for the requested
// response types to params. It delivers a server_error and returns false when
// minting fails.
func (h *AuthorizeHandler) mintTokens(w http.ResponseWriter, u *url.URL, mode string, q url.Values, rt responseTypes, issuedCode, state, subject string, authTime time.Time, params url.Values) bool {
	fail := func(desc string) bool {
		deliver(w, u, mode, errorParams("server_error", desc, state))
		return false
	}
	if h.Token == nil {
		return fail("token minting is not configured")
	}
	clientID := q.Get("client_id")
	cfg := h.Token.Keys.Cfg()

	var access string
	if rt.token {
		var err error
		access, err = h.accessToken(q, subject, clientID)
		if err != nil {
			return fail(err.Error())
		}
		params.Set("access_token", access)
		params.Set("token_type", "Bearer")
		params.Set("expires_in", strconv.FormatInt(int64(cfg.AccessTokenTTL.Seconds()), 10))
		if scope := q.Get("scope"); scope != "" {
			params.Set("scope", scope)
		}
	}
	if rt.idToken {
		idExtra := deriveProfile(subject)
		idExtra["azp"] = clientID
		idExtra["nonce"] = q.Get("nonce")
		idExtra["auth_time"] = authTime.Unix()
		if access != "" {
			idExtra["at_hash"] = tokenHash(access)
		}
		if issuedCode != "" {
			idExtra["c_hash"] = tokenHash(issuedCode)
		}
		id, err := h.Token.Keys.Mint(jwks.MintOpts{
			Subject:  subject,
			Audience: []string{clientID},
			TTL:      cfg.IDTokenTTL,
			Extra:    idExtra,
		})
		if err != nil {
			return fail(err.Error())
		}
		h.Token.rememberProfile(subject, idExtra)
		params.Set("id_token", id)
	} else {
		// Token-only clients read the profile from /userinfo.
		h.Token.rememberProfile(subject, deriveProfile(subject))
	}
	return true
}

// accessToken mints the access token for a response type that includes token.
func (h *AuthorizeHandler) accessToken(q url.Values, subject, clientID string) (string, error) {
	aud := h.Token.resolveAudience(q.Get("audience"))
	params := make(map[string]any, len(q))
	for k := range q {
		params[k] = q.Get(k)
	}
	return h.Token.mintAccess(jwks.MintOpts{
		Subject:  subject,
		Audience: []string{aud},
		Scope:    q.Get("scope"),
		TTL:      h.Token.Keys.Cfg().AccessTokenTTL,
		Extra:    h.Token.augmentExtra(map[string]any{"azp": clientID}, aud, params),
	}, clientID)
}

// recordCode stores the per-code authorization state (nonce, the resolved subject and auth_time and,
// when sent, the validated PKCE challenge) for /oauth/token. Only meaningful
// for the "code" response type.
func (h *AuthorizeHandler) recordCode(q url.Values, issuedCode, subject string, authTime time.Time) {
	if h.PKCE == nil || issuedCode == "" {
		return
	}
	entry := pkce.Entry{
		ClientID:  q.Get("client_id"),
		Redirect:  q.Get("redirect_uri"),
		Nonce:     q.Get("nonce"),
		LoginHint: subject,
		AuthTime:  authTime,
		Audience:  strings.TrimSpace(q.Get("audience")),
		Scope:     q.Get("scope"),
	}
	if challenge := q.Get("code_challenge"); challenge != "" {
		entry.Challenge = challenge
		entry.Method = pkce.Method(q.Get("code_challenge_method"))
		if entry.Method == "" {
			entry.Method = pkce.MethodPlain // RFC 7636 default when method omitted.
		}
	}
	h.PKCE.Put(issuedCode, entry)
}

// errorParams builds the OAuth error response parameters, leaving out the
// description and state when empty.
func errorParams(code, description, state string) url.Values {
	p := url.Values{"error": {code}}
	if description != "" {
		p.Set("error_description", description)
	}
	if state != "" {
		p.Set("state", state)
	}
	return p
}

// isRedirectAllowed shares the opt-in allow-list logic with
// LogoutHandler.isAllowed via isSafeRedirect: empty allow-list = no
// enforcement (the documented test-friendly default — clients can
// register any callback); populated = full Auth0-style allow-list
// with the scheme + backslash + multiple-slash + leading-whitespace
// bypass guards.
func (h *AuthorizeHandler) isRedirectAllowed(raw string) bool {
	if len(h.AllowedRedirectURIs) == 0 {
		return true
	}
	return isSafeRedirect(raw, h.AllowedRedirectURIs)
}
