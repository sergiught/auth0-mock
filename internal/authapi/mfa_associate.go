package authapi

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/bearer"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/mfa"
)

// associateSecret is the fixed otp secret returned for otp enrollment.
const associateSecret = //nolint:gosec // fixed mock value, not a credential
"JBSWY3DPEHPK3PXP"

// MFAAssociateHandler serves POST /mfa/associate. The mfa_token is the
// bearer and is peeked, so it stays redeemable at /oauth/token.
type MFAAssociateHandler struct {
	MFA *mfa.Store
}

type mfaAssociateBody struct {
	AuthenticatorType string   `json:"authenticator_type"`
	Secret            string   `json:"secret,omitempty"`
	BarcodeURI        string   `json:"barcode_uri,omitempty"`
	OOBChannel        string   `json:"oob_channel,omitempty"`
	OOBCode           string   `json:"oob_code,omitempty"`
	BindingMethod     string   `json:"binding_method,omitempty"`
	RecoveryCodes     []string `json:"recovery_codes,omitempty"`
}

func (h *MFAAssociateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.MFA == nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "mfa not configured on this server")
		return
	}
	token, ok := bearer.Token(r.Header.Get("Authorization"))
	if !ok {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
		return
	}
	ctx, ok := h.MFA.Peek(token)
	if !ok {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", "Malformed or expired mfa_token")
		return
	}
	req, err := parseTokenRequest(r)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	add, channel, problem := associatePlan(r, req.Params)
	if problem != "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", problem)
		return
	}

	key, _, found := h.MFA.Lookup(ctx)
	if !found {
		key = ctx.Subject
	}
	enrolled, err := h.MFA.Enroll(key, add)
	if err != nil {
		if errors.Is(err, mfa.ErrAlreadyEnrolled) {
			httperr.WriteAuth(w, http.StatusForbidden, "access_denied", "User is already enrolled.")
			return
		}
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	body := mfaAssociateBody{AuthenticatorType: mfa.TypeOTP}
	if channel == "" {
		body.Secret = associateSecret
	}
	if channel == "" || channel == mfa.ChannelPush {
		body.BarcodeURI = barcodeURI(r, ctx)
	}
	if channel != "" {
		body.AuthenticatorType = mfa.TypeOOB
		body.OOBChannel = channel
		body.OOBCode = h.MFA.IssueOOB(token, enrolled[0].ID)
		if channel != mfa.ChannelPush {
			body.BindingMethod = "prompt"
		}
	}
	if slices.ContainsFunc(enrolled, func(a mfa.AuthenticatorConfig) bool { return a.Type == mfa.TypeRecoveryCode }) {
		body.RecoveryCodes = []string{mfa.AcceptedRecoveryCode}
	}
	render.JSON(w, r, body)
}

// associatePlan validates the request and returns the authenticators to
// enroll and the oob channel ("" for otp). Problem is non-empty when the
// request is invalid.
func associatePlan(r *http.Request, params map[string]any) (add []mfa.AuthenticatorConfig, channel, problem string) {
	types := formList(r, params, "authenticator_types")
	if len(types) != 1 || (types[0] != mfa.TypeOTP && types[0] != mfa.TypeOOB) {
		return nil, "", `authenticator_types must be ["otp"] or ["oob"]`
	}
	if types[0] == mfa.TypeOTP {
		return []mfa.AuthenticatorConfig{{Type: mfa.TypeOTP}}, "", ""
	}
	channels := formList(r, params, "oob_channels")
	if len(channels) != 1 || !slices.Contains(
		[]string{mfa.ChannelSMS, mfa.ChannelVoice, mfa.ChannelEmail, mfa.ChannelPush}, channels[0]) {
		return nil, "", "oob_channels must be one of sms, voice, email, auth0"
	}
	channel = channels[0]
	phone, _ := params["phone_number"].(string)
	email, _ := params["email"].(string)
	switch channel {
	case mfa.ChannelSMS, mfa.ChannelVoice:
		if phone == "" {
			return nil, "", "phone_number is required for the sms and voice channels"
		}
		return []mfa.AuthenticatorConfig{{Type: mfa.TypeOOB, OOBChannel: channel, Name: phone}}, channel, ""
	case mfa.ChannelEmail:
		if email == "" {
			return nil, "", "email is required for the email channel"
		}
		return []mfa.AuthenticatorConfig{{Type: mfa.TypeOOB, OOBChannel: channel, Name: email}}, channel, ""
	}
	return []mfa.AuthenticatorConfig{
		{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelPush},
		{Type: mfa.TypeOTP},
	}, channel, ""
}

func barcodeURI(r *http.Request, ctx mfa.Context) string {
	tenant, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		tenant = r.Host
	}
	user := ctx.Login
	if user == "" {
		user = ctx.Subject
	}
	return "otpauth://totp/" + tenant + ":" + url.PathEscape(user) +
		"?secret=" + associateSecret + "&issuer=" + tenant + "&algorithm=SHA1&digits=6&period=30"
}

// formList reads a list parameter: repeated form values when present, else
// the parsed params value (a JSON array or a single string).
func formList(r *http.Request, params map[string]any, name string) []string {
	if vs := r.PostForm[name]; len(vs) > 0 {
		return vs
	}
	list, _ := stringList(params[name])
	return list
}

// stringList normalises a JSON array of strings or a single string. Ok is
// false for any other shape.
func stringList(v any) ([]string, bool) {
	switch t := v.(type) {
	case string:
		return []string{t}, true
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, isString := e.(string)
			if !isString {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}
