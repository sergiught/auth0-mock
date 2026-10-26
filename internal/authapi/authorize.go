package authapi

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/pkce"
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
	if responseType == "" {
		responseType = "code"
	}

	u, err := url.Parse(redirect)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "invalid redirect_uri")
		return
	}
	if !h.isRedirectAllowed(redirect) {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request",
			"redirect_uri is not in the configured allow-list (AUTHORIZE_ALLOWED_CALLBACKS); add it there to permit this redirect")
		return
	}
	mode := resolveMode(q.Get("response_mode"), responseType == "token")

	// Validate PKCE challenge length up-front. RFC 7636 §4.1 requires the
	// code_verifier (and therefore the plain code_challenge, or the pre-hash
	// for S256) to be 43..128 characters. Rejecting out-of-range values at
	// /authorize gives the client a real error instead of a code that
	// silently fails to exchange later. Validation runs even when the PKCE
	// store isn't wired so client-side bugs surface either way.
	if challenge := q.Get("code_challenge"); challenge != "" {
		if n := len(challenge); n < 43 || n > 128 {
			deliver(w, u, mode, errorParams("invalid_request",
				fmt.Sprintf("code_challenge must be 43..128 characters (RFC 7636 section 4.1), got %d", n), state))
			return
		}
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

	params := url.Values{}
	var issuedCode string
	switch responseType {
	case "token":
		params.Set("access_token", "mock-implicit-token-"+uuid.NewString())
		params.Set("token_type", "Bearer")
	default:
		issuedCode = uuid.NewString()
		params.Set("code", issuedCode)
	}
	if state != "" {
		params.Set("state", state)
	}

	h.recordCode(q, issuedCode)

	deliver(w, u, mode, params)
}

// recordCode stores the per-code authorization state (nonce, login_hint and,
// when sent, the validated PKCE challenge) for /oauth/token. Only meaningful
// for the "code" response type.
func (h *AuthorizeHandler) recordCode(q url.Values, issuedCode string) {
	if h.PKCE == nil || issuedCode == "" {
		return
	}
	entry := pkce.Entry{
		ClientID:  q.Get("client_id"),
		Redirect:  q.Get("redirect_uri"),
		Nonce:     q.Get("nonce"),
		LoginHint: q.Get("login_hint"),
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
