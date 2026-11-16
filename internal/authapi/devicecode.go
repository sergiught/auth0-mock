package authapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/session"
)

const (
	// The grant type of the RFC 8628 device authorization grant.
	grantDeviceCode = "urn:ietf:params:oauth:grant-type:device_code" //nolint:gosec // grant-type URI, not a credential
	// How long a device code and its user code stay valid.
	deviceCodeTTL = 900 * time.Second
	// The minimum polling interval in seconds.
	deviceInterval = 5
)

// DeviceCodeHandler serves POST /oauth/device/code (RFC 8628 §3.1).
type DeviceCodeHandler struct {
	Token   *TokenHandler
	Devices *session.Store
	// LoginPage makes entries start pending, for /activate to approve. Off,
	// they start approved for DefaultSubject.
	LoginPage bool
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func (h *DeviceCodeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenRequest(r)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.ClientID) == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "Missing required parameter: client_id")
		return
	}
	now := h.Token.Keys.Cfg().Now()
	d := session.Device{
		ClientID: req.ClientID,
		Audience: h.Token.resolveAudience(req.Audience),
		Scope:    req.Scope,
		Expires:  now.Add(deviceCodeTTL),
	}
	if !h.LoginPage {
		d.Status = session.DeviceApproved
		d.Subject = DefaultSubject
		d.AuthTime = now
	}
	d = h.Devices.StartDevice(d)
	uri := h.Token.Keys.IssuerFor(r)
	uri = strings.TrimSuffix(uri, "/") + "/activate"
	render.JSON(w, r, deviceCodeResponse{
		DeviceCode:              d.DeviceCode,
		UserCode:                d.UserCode,
		VerificationURI:         uri,
		VerificationURIComplete: uri + "?user_code=" + d.UserCode,
		ExpiresIn:               int(deviceCodeTTL.Seconds()),
		Interval:                deviceInterval,
	})
}
