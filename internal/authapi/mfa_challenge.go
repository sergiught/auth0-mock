package authapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/mfa"
)

const challengeUnsupportedDesc = "User does not have an authenticator for the requested challenge types"

// MFAChallengeHandler serves POST /mfa/challenge. It peeks the mfa_token
// without consuming it, so the same token can still be redeemed afterwards.
type MFAChallengeHandler struct {
	MFA *mfa.Store
}

// mfaChallengeBody is the /mfa/challenge response. Oob_code and
// binding_method only appear on an oob challenge.
type mfaChallengeBody struct {
	ChallengeType string `json:"challenge_type"`
	OOBCode       string `json:"oob_code,omitempty"`
	BindingMethod string `json:"binding_method,omitempty"`
}

func (h *MFAChallengeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenRequest(r)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if h.MFA == nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "mfa not configured on this server")
		return
	}
	if req.MFAToken == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing mfa_token")
		return
	}
	ctx, ok := h.MFA.Peek(req.MFAToken)
	if !ok {
		httperr.WriteAuth(w, http.StatusForbidden, "invalid_grant", "Malformed or expired mfa_token")
		return
	}

	if field := nonStringParam(req.Params, "challenge_type", "authenticator_id"); field != "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", field+" must be a string")
		return
	}
	raw, _ := req.Params["challenge_type"].(string)
	types := strings.Fields(raw)
	if len(types) == 0 {
		types = []string{mfa.TypeOTP, mfa.TypeOOB}
	}
	for _, t := range types {
		if t != mfa.TypeOTP && t != mfa.TypeOOB {
			httperr.WriteAuth(w, http.StatusBadRequest, "unsupported_challenge_type", "Unsupported challenge type: "+t)
			return
		}
	}

	_, cfg, strict := h.MFA.Lookup(ctx)
	if !strict {
		body := mfaChallengeBody{ChallengeType: mfa.TypeOTP}
		if !slices.Contains(types, mfa.TypeOTP) {
			body = mfaChallengeBody{
				ChallengeType: mfa.TypeOOB,
				OOBCode:       h.MFA.IssueOOB(req.MFAToken, ""),
				BindingMethod: "prompt",
			}
		}
		render.JSON(w, r, body)
		return
	}

	chosen, rejection := pickAuthenticator(cfg, types, req.Params["authenticator_id"])
	if rejection != nil {
		httperr.WriteAuth(w, rejection.status, rejection.code, rejection.desc)
		return
	}
	if chosen.Type == mfa.TypeOTP {
		render.JSON(w, r, mfaChallengeBody{ChallengeType: mfa.TypeOTP})
		return
	}
	body := mfaChallengeBody{
		ChallengeType: mfa.TypeOOB,
		OOBCode:       h.MFA.IssueOOB(req.MFAToken, chosen.ID),
	}
	if chosen.OOBChannel != mfa.ChannelPush {
		body.BindingMethod = "prompt"
	}
	render.JSON(w, r, body)
}

// pickAuthenticator chooses the authenticator a strict user is challenged
// with: the one named by authenticator_id, else the first active otp/oob
// entry whose type was requested.
func pickAuthenticator(cfg mfa.UserConfig, types []string, id any) (mfa.AuthenticatorConfig, *mfaRejection) {
	unsupported := &mfaRejection{http.StatusBadRequest, "unsupported_challenge_type", challengeUnsupportedDesc}
	active := func(a mfa.AuthenticatorConfig) bool { return a.Active != nil && *a.Active }
	challengeable := func(a mfa.AuthenticatorConfig) bool { return a.Type == mfa.TypeOTP || a.Type == mfa.TypeOOB }

	if wanted, _ := id.(string); wanted != "" {
		i := slices.IndexFunc(cfg.Authenticators, func(a mfa.AuthenticatorConfig) bool {
			return a.ID == wanted && active(a)
		})
		if i < 0 {
			return mfa.AuthenticatorConfig{}, &mfaRejection{http.StatusBadRequest, "invalid_request", "Authenticator not found"}
		}
		a := cfg.Authenticators[i]
		if !challengeable(a) || !slices.Contains(types, a.Type) {
			return mfa.AuthenticatorConfig{}, unsupported
		}
		return a, nil
	}

	if !activeAuthenticator(cfg, challengeable) {
		return mfa.AuthenticatorConfig{}, &mfaRejection{
			http.StatusBadRequest, "association_required",
			"User is not enrolled. You can use /mfa/associate endpoint to enroll the first authenticator.",
		}
	}
	i := slices.IndexFunc(cfg.Authenticators, func(a mfa.AuthenticatorConfig) bool {
		return active(a) && challengeable(a) && slices.Contains(types, a.Type)
	})
	if i < 0 {
		return mfa.AuthenticatorConfig{}, unsupported
	}
	return cfg.Authenticators[i], nil
}

// nonStringParam returns the first of names that is present in params with a
// value other than a string, or "" when there is none.
func nonStringParam(params map[string]any, names ...string) string {
	for _, name := range names {
		if v, present := params[name]; present {
			if _, isString := v.(string); !isString {
				return name
			}
		}
	}
	return ""
}
