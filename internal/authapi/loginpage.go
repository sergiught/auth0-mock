package authapi

import (
	"html/template"
	"net/http"
	"net/url"
	"time"
)

const (
	// LoginPagePath is the login page path, relative so that a /ns/<id>/
	// prefix on /authorize carries over to it.
	loginPagePath = "u/login"
	// ErrInvalidLogin is shown when the transaction id is unknown, used or expired.
	errInvalidLogin = "Invalid or expired login"
	// PendingLoginTTL is how long a login page link stays valid on the mock clock.
	pendingLoginTTL = 10 * time.Minute
)

// LoginPageHandler serves the opt-in login page at /u/login.
type LoginPageHandler struct {
	Authorize *AuthorizeHandler
}

// ServeHTTP renders the form on GET. POST is answered later.
func (h *LoginPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.get(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *LoginPageHandler) get(w http.ResponseWriter, r *http.Request) {
	a := h.Authorize
	var q url.Values
	ok := false
	if a.Sessions != nil {
		q, ok = a.Sessions.Pending(r.URL.Query().Get("state"), a.now())
	}
	if !ok {
		renderLoginError(w, http.StatusBadRequest, errInvalidLogin)
		return
	}
	hint := ""
	if v := q["login_hint"]; len(v) > 0 {
		hint = v[0]
	}
	client := ""
	if v := q["client_id"]; len(v) > 0 {
		client = v[0]
	}
	renderLoginPage(w, http.StatusOK, client, hint, "")
}

var loginPageTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Log in</title>
<style>
body{font-family:system-ui,sans-serif;background:#f4f5f7;margin:0;display:flex;justify-content:center;padding:48px 16px}
main{background:#fff;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.12);padding:32px;width:100%;max-width:360px}
h1{margin:0 0 8px;font-size:24px}
p{margin:0 0 20px;color:#555}
label{display:block;margin:12px 0 4px;font-size:14px}
input{box-sizing:border-box;width:100%;padding:10px;border:1px solid #bbb;border-radius:4px;font-size:16px}
button{margin-top:20px;width:100%;padding:12px;border:0;border-radius:4px;background:#1f2933;color:#fff;font-size:16px;cursor:pointer}
.error{color:#b00020;margin:0 0 12px}
</style>
</head>
<body>
<main>
{{if .Form}}<h1>Log in</h1>
<p>to continue to {{.Client}}</p>
<form method="POST" action="">
{{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}
<label for="username">Email address</label>
<input id="username" name="username" type="text" value="{{.Username}}" autocomplete="username" autofocus>
<label for="password">Password</label>
<input id="password" name="password" type="password" autocomplete="current-password">
<button type="submit" name="action" value="default">Continue</button>
</form>{{else}}<h1>Error</h1>
<div class="error" role="alert">{{.Error}}</div>{{end}}
</main>
</body>
</html>
`))

// renderLoginPage writes the login form, with errMsg above the fields when set.
// Escaping is left to html/template.
func renderLoginPage(w http.ResponseWriter, status int, clientID, username, errMsg string) {
	writeLoginPage(w, status, loginPageData{Form: true, Client: clientID, Username: username, Error: errMsg})
}

// renderLoginError writes a bare error page with no form, for an unusable transaction.
func renderLoginError(w http.ResponseWriter, status int, errMsg string) {
	writeLoginPage(w, status, loginPageData{Error: errMsg})
}

type loginPageData struct {
	Form                    bool
	Client, Username, Error string
}

func writeLoginPage(w http.ResponseWriter, status int, data loginPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// The status line is already sent, so a failed write to the client cannot be reported.
	_ = loginPageTmpl.Execute(w, data)
}
