package middleware

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// corsWildcard is an allow-list entry such as https://*.example.com:8443.
type corsWildcard struct {
	scheme string
	suffix string
	port   string
}

// corsDefaultPorts are the ports browsers omit from the Origin header.
var corsDefaultPorts = map[string]string{"http": "80", "https": "443"}

// CORS returns middleware that answers cross-origin browser requests. An empty
// allow-list or a "*" entry allows any origin; otherwise entries are
// scheme://host[:port] or scheme://*.host[:port], where the wildcard is exactly
// one leading DNS label. Allowed origins are echoed with credentials enabled,
// preflights are answered with 204, and disallowed preflights get 403.
func CORS(allowedOrigins []string) (func(http.Handler) http.Handler, error) {
	m, err := newCORSMatcher(allowedOrigins)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Add("Vary", "Origin")
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if !m.allowed(origin) {
				if preflight {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			if !preflight {
				next.ServeHTTP(w, r)
				return
			}
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			if rh := r.Header.Get("Access-Control-Request-Headers"); rh != "" {
				h.Set("Access-Control-Allow-Headers", rh)
			}
			h.Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
		})
	}, nil
}

// corsMatcher holds the parsed origin allow-list.
type corsMatcher struct {
	anyOrigin bool
	exact     map[string]bool
	wildcards []corsWildcard
}

// parseCORSEntry parses a lower-cased allow-list entry, rejecting shapes that
// can never match a browser Origin.
func parseCORSEntry(e string) (*url.URL, error) {
	u, err := url.Parse(e)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(e, "?#") ||
		strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid origin %q", e)
	}
	return u, nil
}

// newCORSMatcher parses and validates the allow-list entries once.
func newCORSMatcher(entries []string) (*corsMatcher, error) {
	m := &corsMatcher{anyOrigin: len(entries) == 0, exact: map[string]bool{}}
	for _, raw := range entries {
		e := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
		if e == "*" {
			m.anyOrigin = true
			continue
		}
		u, err := parseCORSEntry(e)
		if err != nil {
			return nil, fmt.Errorf("invalid CORS origin %q: want scheme://host[:port]", raw)
		}
		port := u.Port()
		if port == corsDefaultPorts[u.Scheme] {
			port = ""
		}
		host := u.Hostname()
		if !strings.Contains(host, "*") {
			key := u.Scheme + "://" + strings.TrimSuffix(u.Host, ":"+u.Port())
			if port != "" {
				key += ":" + port
			}
			m.exact[key] = true
			continue
		}
		suffix, ok := strings.CutPrefix(host, "*.")
		if !ok || suffix == "" || strings.Contains(suffix, "*") {
			return nil, fmt.Errorf("invalid CORS origin %q: * must be the whole first host label", raw)
		}
		m.wildcards = append(m.wildcards, corsWildcard{scheme: u.Scheme, suffix: suffix, port: port})
	}
	return m, nil
}

// allowed reports whether the request origin matches the allow-list.
func (m *corsMatcher) allowed(origin string) bool {
	if m.anyOrigin {
		return true
	}
	o := strings.ToLower(origin)
	if m.exact[o] {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	for _, w := range m.wildcards {
		label, ok := strings.CutSuffix(host, "."+w.suffix)
		if ok && u.Scheme == w.scheme && u.Port() == w.port && label != "" && !strings.Contains(label, ".") {
			return true
		}
	}
	return false
}
