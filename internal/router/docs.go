package router

import (
	"embed"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// docsFS holds the static assets for the `/docs` API reference page: the HTML
// shell, its stylesheet, and the self-hosted Geist fonts.
//
//go:embed docs
var docsFS embed.FS

// docsIndexHTML is the API reference page, read once from the embedded FS.
var docsIndexHTML = mustReadDocs("docs/index.html")

func mustReadDocs(name string) []byte {
	b, err := docsFS.ReadFile(name)
	if err != nil {
		panic("router: embedded docs asset missing: " + name + ": " + err.Error())
	}
	return b
}

// mountDocs registers the `/docs` HTML page and its static assets on r.
func mountDocs(r chi.Router) {
	r.Method(http.MethodGet, "/docs", http.HandlerFunc(serveDocs))
}

func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(docsIndexHTML)
}
