// Package namespace routes requests to per-namespace handlers so tests can
// share one mock process without sharing state.
package namespace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
)

// Header names the namespace a request belongs to.
const Header = "X-Auth0-Mock-Namespace"

const (
	prefix    = "/ns/"
	listPath  = "/admin0/namespaces"
	itemPath  = "/admin0/namespaces/"
	badReqStr = "Bad Request"
)

var idRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Instance is one namespace's handler and its optional cleanup.
type Instance struct {
	Handler http.Handler
	Close   func(context.Context) // may be nil
}

// Factory builds the instance for a namespace id.
type Factory func(id string) (*Instance, error)

type entry struct {
	inst     *Instance
	created  time.Time
	lastUsed time.Time
}

// Dispatcher picks a namespace per request and forwards to its handler,
// building handlers lazily. Requests naming no namespace go to the default.
type Dispatcher struct {
	def   http.Handler
	build Factory
	max   int

	mu sync.Mutex
	ns map[string]*entry
}

// New returns a Dispatcher. max caps the number of live namespaces; a max
// below 1 means unlimited.
func New(def http.Handler, build Factory, limit int) *Dispatcher {
	return &Dispatcher{def: def, build: build, max: limit, ns: map[string]*entry{}}
}

// ServeHTTP implements http.Handler.
func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, path, rawPath, viaPrefix, ok := resolve(w, r)
	if !ok {
		return
	}

	if path == listPath || strings.HasPrefix(path, itemPath) {
		d.lifecycle(w, r, path)
		return
	}

	r2 := r
	if viaPrefix {
		u := *r.URL
		u.Path, u.RawPath = path, rawPath
		r2 = r.Clone(r.Context())
		r2.URL = &u
	}

	if id == "" || path == "/healthz" || path == "/readyz" {
		d.def.ServeHTTP(w, r2)
		return
	}

	inst, ok := d.acquire(w, id)
	if !ok {
		return
	}
	if viaPrefix {
		r2 = r2.WithContext(jwks.WithIssuerPath(r2.Context(), "ns/"+id+"/"))
	}
	inst.Handler.ServeHTTP(w, r2)
}

// resolve extracts the namespace id and the forwarded path from r. On an
// invalid or conflicting id it has written the response and returns ok=false.
func resolve(w http.ResponseWriter, r *http.Request) (id, path, rawPath string, viaPrefix, ok bool) {
	id = r.Header.Get(Header)
	path, rawPath = r.URL.Path, r.URL.RawPath
	if !strings.HasPrefix(path, prefix) {
		if id != "" && !idRE.MatchString(id) {
			invalid(w, "invalid namespace id "+quote(id)+" in "+Header+" header")
			return "", "", "", false, false
		}
		return id, path, rawPath, false, true
	}
	pid, tail, _ := strings.Cut(path[len(prefix):], "/")
	if !idRE.MatchString(pid) {
		invalid(w, "invalid namespace id "+quote(pid)+" in path prefix")
		return "", "", "", false, false
	}
	if id != "" && id != pid {
		invalid(w, "namespace header "+quote(id)+" differs from path prefix "+quote(pid))
		return "", "", "", false, false
	}
	if rawPath != "" {
		if strings.HasPrefix(rawPath, prefix) {
			rawPath = "/" + afterSegment(rawPath[len(prefix):])
		} else {
			rawPath = "" // prefix arrived percent-encoded; route on Path
		}
	}
	return pid, "/" + tail, rawPath, true, true
}

// acquire returns the instance for id, building it on first use and marking
// it used. On failure it has written the response.
func (d *Dispatcher) acquire(w http.ResponseWriter, id string) (*Instance, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	e, ok := d.ns[id]
	if !ok {
		if d.max > 0 && len(d.ns) >= d.max {
			httperr.WriteMgmt(w, http.StatusTooManyRequests, "Too Many Requests",
				"namespace limit reached (NAMESPACE_MAX="+itoa(d.max)+"); delete unused namespaces with DELETE /admin0/namespaces/{id}",
				"too_many_namespaces")
			return nil, false
		}
		inst, err := d.build(id)
		if err != nil {
			httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
				"building namespace "+quote(id)+" failed: "+err.Error(), "namespace_build_failed")
			return nil, false
		}
		e = &entry{inst: inst, created: now}
		d.ns[id] = e
	}
	e.lastUsed = now
	return e.inst, true
}

func (d *Dispatcher) lifecycle(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	case path == listPath && r.Method == http.MethodGet:
		d.list(w)
	case path == listPath && r.Method == http.MethodDelete:
		d.mu.Lock()
		old := d.ns
		d.ns = map[string]*entry{}
		d.mu.Unlock()
		for _, e := range old {
			closeInstance(r.Context(), e.inst)
		}
		w.WriteHeader(http.StatusNoContent)
	case path != listPath && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(path, itemPath)
		d.mu.Lock()
		e, ok := d.ns[id]
		delete(d.ns, id)
		d.mu.Unlock()
		if !ok {
			httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
				"namespace "+quote(id)+" does not exist", "namespace_not_found")
			return
		}
		closeInstance(r.Context(), e.inst)
		w.WriteHeader(http.StatusNoContent)
	default:
		httperr.WriteMgmt(w, http.StatusMethodNotAllowed, "Method Not Allowed",
			"method "+r.Method+" is not allowed on "+path, "method_not_allowed")
	}
}

type listItem struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

func (d *Dispatcher) list(w http.ResponseWriter) {
	d.mu.Lock()
	items := make([]listItem, 0, len(d.ns))
	for id, e := range d.ns {
		items = append(items, listItem{
			ID:         id,
			CreatedAt:  e.created.UTC().Format(time.RFC3339),
			LastUsedAt: e.lastUsed.UTC().Format(time.RFC3339),
		})
	}
	d.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, items)
}

func closeInstance(ctx context.Context, i *Instance) {
	if i.Close != nil {
		i.Close(ctx)
	}
}

func invalid(w http.ResponseWriter, msg string) {
	httperr.WriteMgmt(w, http.StatusBadRequest, badReqStr, msg, "invalid_namespace")
}

// afterSegment returns s with its first "/"-delimited segment removed.
func afterSegment(s string) string {
	_, tail, _ := strings.Cut(s, "/")
	return tail
}

func quote(s string) string { return url.QueryEscape(s) }

func itoa(n int) string { return strconv.Itoa(n) }

func writeJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
