package mgmtapi

import (
	"net/http"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/spec"
)

func genericHandler(op spec.Operation, opts MountOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Validate request against spec.
		if err := opts.Validator.ValidateRequest(r, op); err != nil {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_request")
			return
		}
		// 2. Find a registered match.
		m := opts.Store.Find(r.Method, r.URL.Path, op.Template)
		if m == nil {
			httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
				"no registered match for "+r.Method+" "+r.URL.Path, "no_match")
			return
		}
		// 3. Defense-in-depth response validation.
		if err := opts.Validator.ValidateResponse(op, m.Status, m.Headers, m.Body); err != nil {
			opts.Log.Error().
				Str("op", op.Op.OperationID).
				Int("status", m.Status).
				Err(err).
				Msg("registered match violates response schema")
			if opts.Strict {
				httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
					"registered match violates schema: "+err.Error(), "invalid_match")
				return
			}
		}
		// 4. Write the registered response.
		for k, v := range m.Headers {
			w.Header().Set(k, v)
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(m.Status)
		_, _ = w.Write(m.Body)
	})
}
