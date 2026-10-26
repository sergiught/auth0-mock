package authapi

import (
	"html/template"
	"net/http"
	"net/url"
	"sort"
)

const (
	modeQuery    = "query"
	modeFragment = "fragment"
	modeFormPost = "form_post"

	modeWebMessage = "web_message"
)

// resolveMode picks how /authorize delivers its result: the explicit
// response_mode when it is one the mock implements, otherwise the OAuth
// default: fragment for tokens, query for codes. Other values fall back to
// that default.
func resolveMode(responseMode string, issuesToken bool) string {
	switch responseMode {
	case modeQuery, modeFragment, modeFormPost, modeWebMessage:
		return responseMode
	}
	if issuesToken {
		return modeFragment
	}
	return modeQuery
}

// deliver writes params to redirect per mode: a 302 Location for query and
// fragment, an auto-submitting HTML form for form_post, a postMessage page for
// web_message.
func deliver(w http.ResponseWriter, redirect *url.URL, mode string, params url.Values) {
	switch mode {
	case modeFragment:
		u := *redirect
		u.Fragment, u.RawFragment = "", ""
		w.Header().Set("Location", u.String()+"#"+params.Encode())
		w.WriteHeader(http.StatusFound)
	case modeFormPost:
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fields := make([]formField, 0, len(keys))
		for _, k := range keys {
			fields = append(fields, formField{Name: k, Value: params.Get(k)})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = formPostTemplate.Execute(w, struct {
			Action string
			Fields []formField
		}{redirect.String(), fields})
	case modeWebMessage:
		response := make(map[string]string, len(params))
		for k := range params {
			response[k] = params.Get(k)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = webMessageTemplate.Execute(w, struct {
			Origin   string
			Response map[string]string
		}{redirect.Scheme + "://" + redirect.Host, response})
	default:
		u := *redirect
		q := u.Query()
		for k, vs := range params {
			q[k] = vs
		}
		u.RawQuery = q.Encode()
		w.Header().Set("Location", u.String())
		w.WriteHeader(http.StatusFound)
	}
}

type formField struct{ Name, Value string }

var formPostTemplate = template.Must(template.New("form_post").Parse(`<!DOCTYPE html>
<html><head><title>Submitting...</title></head>
<body onload="document.forms[0].submit()">
<form method="post" action="{{.Action}}">
{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{end}}<noscript><button type="submit">Continue</button></noscript>
</form>
</body></html>
`))

var webMessageTemplate = template.Must(template.New("web_message").Parse(`<!DOCTYPE html>
<html><head><title>Authorization Response</title></head>
<body><script type="text/javascript">
(function(window, document) {
  var targetOrigin = {{.Origin}};
  var authorizationResponse = {type: "authorization_response", response: {{.Response}}};
  var mainWin = (window.opener) ? window.opener : window.parent;
  mainWin.postMessage(authorizationResponse, targetOrigin);
})(this, this.document);
</script></body></html>
`))
