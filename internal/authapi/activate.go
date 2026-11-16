package authapi

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sergiught/auth0-mock/internal/session"
)

// errInvalidCode is shown when a user code or activation step is unusable.
const errInvalidCode = "Invalid or expired code"

// ActivateHandler serves the device activation page at /activate.
type ActivateHandler struct {
	Authorize *AuthorizeHandler
	Devices   *session.Store
}

// ServeHTTP renders the code form on GET and runs the activation steps on POST.
func (h *ActivateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeActivate(w, http.StatusOK, activateData{View: "code", Code: r.URL.Query().Get("user_code")})
	case http.MethodPost:
		h.post(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ActivateHandler) post(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeActivate(w, http.StatusBadRequest, activateData{View: "error", Error: errInvalidCode})
		return
	}
	state, action := r.PostFormValue("state"), r.PostFormValue("action")
	switch {
	case state == "":
		h.code(w, r)
	case action == "default":
		h.login(w, r, state)
	case action == "confirm" || action == "cancel":
		h.decide(w, state, action == "confirm")
	default:
		writeActivate(w, http.StatusBadRequest, activateData{View: "error", Error: errInvalidCode})
	}
}

func (h *ActivateHandler) code(w http.ResponseWriter, r *http.Request) {
	typed := r.PostFormValue("code")
	now := h.Authorize.now()
	dev, ok := h.Devices.DeviceByUserCode(typed, now)
	if !ok {
		writeActivate(w, http.StatusBadRequest, activateData{View: "code", Code: typed, Error: errInvalidCode})
		return
	}
	if _, login, live := currentLogin(r, h.Authorize.Sessions); live {
		step := h.Devices.SetDeviceStep(dev.DeviceCode, login.Subject, login.AuthTime)
		writeActivate(w, http.StatusOK, activateData{View: "confirm", State: step, Code: dev.UserCode})
		return
	}
	step := h.Devices.SetDeviceStep(dev.DeviceCode, "", time.Time{})
	writeActivate(w, http.StatusOK, activateData{View: "login", State: step, Client: dev.ClientID})
}

func (h *ActivateHandler) login(w http.ResponseWriter, r *http.Request, state string) {
	a := h.Authorize
	now := a.now()
	dev, ok := h.Devices.DeviceByStep(state, now)
	if !ok || dev.Subject != "" {
		writeActivate(w, http.StatusBadRequest, activateData{View: "error", Error: errInvalidCode})
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	fail := func(msg string) {
		writeActivate(w, http.StatusBadRequest, activateData{View: "login", State: state, Client: dev.ClientID, Username: username, Error: msg})
	}
	if username == "" {
		fail(errEmailRequired)
		return
	}
	if r.PostFormValue("password") == "" {
		fail(errPasswordRequired)
		return
	}
	if a.Errors != nil {
		q := url.Values{
			"client_id": {dev.ClientID}, "audience": {dev.Audience}, "scope": {dev.Scope}, "username": {username},
		}
		if rule := a.Errors.Find(q); rule != nil {
			msg := rule.ErrorDescription
			if msg == "" {
				msg = errWrongLogin
			}
			fail(msg)
			return
		}
	}
	dev, ok = h.Devices.TakeDeviceStep(state, now)
	if !ok {
		writeActivate(w, http.StatusBadRequest, activateData{View: "error", Error: errInvalidCode})
		return
	}
	a.startSession(w, r, username, now)
	step := h.Devices.SetDeviceStep(dev.DeviceCode, username, now)
	writeActivate(w, http.StatusOK, activateData{View: "confirm", State: step, Code: dev.UserCode})
}

func (h *ActivateHandler) decide(w http.ResponseWriter, state string, confirm bool) {
	dev, ok := h.Devices.TakeDeviceStep(state, h.Authorize.now())
	if !ok || dev.Subject == "" {
		writeActivate(w, http.StatusBadRequest, activateData{View: "error", Error: errInvalidCode})
		return
	}
	if confirm {
		h.Devices.ApproveDevice(dev.DeviceCode, dev.Subject, dev.AuthTime)
		writeActivate(w, http.StatusOK, activateData{View: "done"})
		return
	}
	h.Devices.DenyDevice(dev.DeviceCode)
	writeActivate(w, http.StatusOK, activateData{View: "cancelled"})
}

type activateData struct {
	View                          string
	State, Code, Client, Username string
	Error                         string
}

var activateTmpl = template.Must(template.New("activate").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Activate your device</title>
<style>
` + pageCSS + `</style>
</head>
<body>
<main>
{{if eq .View "code"}}<h1>Activate your device</h1>
<p>Enter the code displayed on your device</p>
<form method="POST" action="">
{{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}
<label for="code">Code</label>
<input id="code" name="code" type="text" value="{{.Code}}" autofocus>
<button type="submit" name="action" value="default">Continue</button>
</form>{{else if eq .View "login"}}<h1>Log in</h1>
<p>to continue to {{.Client}}</p>
<form method="POST" action="">
{{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}
<input type="hidden" name="state" value="{{.State}}">
<label for="username">Email address</label>
<input id="username" name="username" type="text" value="{{.Username}}" autocomplete="username" autofocus>
<label for="password">Password</label>
<input id="password" name="password" type="password" autocomplete="current-password">
<button type="submit" name="action" value="default">Continue</button>
</form>{{else if eq .View "confirm"}}<h1>Device Confirmation</h1>
<p>Please confirm this is the code displayed on your device</p>
<p><strong>{{.Code}}</strong></p>
<form method="POST" action="">
<input type="hidden" name="state" value="{{.State}}">
<button type="submit" name="action" value="confirm">Confirm</button>
<button type="submit" name="action" value="cancel">Cancel</button>
</form>{{else if eq .View "done"}}<h1>Congratulations, you're all set!</h1>
<p>Your device is now connected.</p>{{else if eq .View "cancelled"}}<h1>Device activation cancelled</h1>
<p>Your device was not connected.</p>{{else}}<h1>Error</h1>
<div class="error" role="alert">{{.Error}}</div>{{end}}
</main>
</body>
</html>
`))

func writeActivate(w http.ResponseWriter, status int, data activateData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// The status line is already sent, so a failed write to the client cannot be reported.
	_ = activateTmpl.Execute(w, data)
}
