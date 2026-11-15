// Package pathparam reads chi URL parameters that may hold escaped bytes.
package pathparam

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// Get reads the named path parameter, decoded exactly once. Chi routes on
// RawPath only when it is set, so the param is still escaped then; otherwise
// it comes from the already-decoded Path. It returns an error when the
// escape is malformed.
func Get(r *http.Request, name string) (string, error) {
	v := chi.URLParam(r, name)
	if r.URL.RawPath == "" {
		return v, nil
	}
	return url.PathUnescape(v)
}
