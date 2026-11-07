package admin0

import (
	"net/http"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/seed"
)

// ExportHandler serves GET /admin0/export: the current mock state in the
// seed file format, as JSON (default) or YAML (?format=yaml). ?detail=true
// adds each expectation's id and hit count.
type ExportHandler struct {
	Deps Deps
}

func (h *ExportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	contentType := "application/json"
	asYAML := false
	switch q.Get("format") {
	case "", "json":
	case "yaml":
		contentType, asYAML = "application/yaml", true
	default:
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"format must be json or yaml", "invalid_query")
		return
	}
	detail := false
	if v := q.Get("detail"); v != "" {
		var ok bool
		if detail, ok = parseFlag(v); !ok {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				"detail must be true or false", "invalid_query")
			return
		}
	}

	var f seed.File
	if h.Deps.Seed != nil {
		// Reset and reload hold the seed lock between wipe and apply.
		f = h.Deps.Seed.Export(detail)
	} else {
		d := h.Deps
		f = seed.Export(seed.Stores{
			Matches:       d.Matches,
			Claims:        d.Claims,
			ClaimMappings: d.ClaimMappings,
			Permissions:   d.Permissions,
			MFA:           d.MFA,
		}, detail)
	}
	var (
		out []byte
		err error
	)
	if asYAML {
		out, err = f.YAML()
	} else {
		out, err = f.JSON()
	}
	if err != nil {
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
			"could not render the export: "+err.Error(), "internal_error")
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(out) //nolint:gosec // JSON or YAML, never rendered as HTML; Content-Type is set above
}
