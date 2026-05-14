package router

import (
	"embed"
	"net/http"
	"path"

	"github.com/go-chi/chi/v5"
)

// docsFS holds the static assets for the `/docs` API reference page: the HTML
// shell, its stylesheet, and the self-hosted Geist fonts.
//
//go:embed docs
var docsFS embed.FS

// docsIndexHTML is the API reference page, read once from the embedded FS.
var docsIndexHTML = mustReadDocs("docs/index.html")

// docsCSS is the stylesheet for the API reference page, read once from the embedded FS.
var docsCSS = mustReadDocs("docs/docs.css")

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
	r.Method(http.MethodGet, "/docs/docs.css", http.HandlerFunc(serveDocsCSS))
	r.Method(http.MethodGet, "/docs/fonts/{file}", http.HandlerFunc(serveDocsFont))
}

func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(docsIndexHTML)
}

func serveDocsCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(docsCSS)
}

// serveDocsFont serves a vendored .woff2 from docs/fonts. path.Base strips any
// directory components, so the {file} param can't escape the fonts directory.
func serveDocsFont(w http.ResponseWriter, r *http.Request) {
	name := path.Base(chi.URLParam(r, "file"))
	b, err := docsFS.ReadFile("docs/fonts/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	_, _ = w.Write(b)
}
