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

	"github.com/rs/zerolog"

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
	Close   func(context.Context) // May be nil.
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
	log   zerolog.Logger
	mws   []func(http.Handler) http.Handler

	mu sync.Mutex
	ns map[string]*entry
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithMiddleware wraps only the responses the Dispatcher writes itself
// (errors and the lifecycle routes), outermost first. Requests forwarded to
// the default or a namespace handler are not wrapped.
func WithMiddleware(mws ...func(http.Handler) http.Handler) Option {
	return func(d *Dispatcher) { d.mws = append(d.mws, mws...) }
}

// WithLogger sets the logger for build failures and limit rejections.
func WithLogger(l zerolog.Logger) Option {
	return func(d *Dispatcher) { d.log = l }
}

// New returns a Dispatcher. The limit caps the number of live namespaces; a
// limit below 1 means unlimited.
func New(def http.Handler, build Factory, limit int, opts ...Option) *Dispatcher {
	d := &Dispatcher{def: def, build: build, max: limit, log: zerolog.Nop(), ns: map[string]*entry{}}
	for _, o := range opts {
		o(d)
	}
	return d
}

// own serves a response the Dispatcher writes itself through its middleware.
func (d *Dispatcher) own(w http.ResponseWriter, r *http.Request, fn func(http.ResponseWriter, *http.Request)) {
	var h http.Handler = http.HandlerFunc(fn)
	for i := len(d.mws) - 1; i >= 0; i-- {
		h = d.mws[i](h)
	}
	h.ServeHTTP(w, r)
}

// ServeHTTP implements http.Handler.
func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Lifecycle routes are global: they ignore the namespace header and
	// prefix for selection, so they are matched before either is validated.
	if p := unprefixed(r.URL.Path); p == listPath || strings.HasPrefix(p, itemPath) {
		d.own(w, r, func(w http.ResponseWriter, r *http.Request) {
			named := r.Header.Get(Header) != "" || strings.HasPrefix(r.URL.Path, prefix)
			d.lifecycle(w, r, p, named)
		})
		return
	}

	id, path, rawPath, viaPrefix, msg := resolve(r)
	if msg != "" {
		d.own(w, r, func(w http.ResponseWriter, _ *http.Request) { invalid(w, msg) })
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

	inst, aerr := d.acquire(id)
	if aerr != nil {
		d.own(w, r, func(w http.ResponseWriter, _ *http.Request) {
			httperr.WriteMgmt(w, aerr.status, aerr.errStr, aerr.message, aerr.code)
		})
		return
	}
	if viaPrefix {
		r2 = r2.WithContext(jwks.WithIssuerPath(r2.Context(), "ns/"+id+"/"))
	}
	inst.Handler.ServeHTTP(w, r2)
}

// unprefixed returns path without a leading "/ns/<id>" segment pair.
func unprefixed(path string) string {
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	return "/" + afterSegment(path[len(prefix):])
}

// resolve extracts the namespace id and the forwarded path from r. A
// non-empty msg means the id is invalid or conflicting.
func resolve(r *http.Request) (id, path, rawPath string, viaPrefix bool, msg string) {
	id = r.Header.Get(Header)
	path, rawPath = r.URL.Path, r.URL.RawPath
	if !strings.HasPrefix(path, prefix) {
		if id != "" && !idRE.MatchString(id) {
			return "", "", "", false, "invalid namespace id " + quote(id) + " in " + Header + " header"
		}
		return id, path, rawPath, false, ""
	}
	pid, tail, _ := strings.Cut(path[len(prefix):], "/")
	if !idRE.MatchString(pid) {
		return "", "", "", false, "invalid namespace id \"" + quote(pid) + "\" in path prefix"
	}
	if id != "" && id != pid {
		return "", "", "", false, "namespace header " + quote(id) + " differs from path prefix " + quote(pid)
	}
	if rawPath != "" {
		rawPath = unprefixedRaw(r.URL.EscapedPath(), pid)
	}
	return pid, "/" + tail, rawPath, true, ""
}

// unprefixedRaw strips the "/ns/<pid>" segments from the escaped path, which
// may spell them percent-encoded. It returns "" (route on Path) when the
// prefix itself contains an encoded slash.
func unprefixedRaw(esc, pid string) string {
	parts := strings.SplitN(esc, "/", 4)
	if len(parts) < 3 {
		return ""
	}
	s1, err1 := url.PathUnescape(parts[1])
	s2, err2 := url.PathUnescape(parts[2])
	if err1 != nil || err2 != nil || s1 != "ns" || s2 != pid {
		return ""
	}
	if len(parts) == 3 {
		return "/"
	}
	return "/" + parts[3]
}

// acquireErr is a response acquire could not avoid.
type acquireErr struct {
	status  int
	errStr  string
	message string
	code    string
}

// acquire returns the instance for id, building it on first use and marking
// it used.
func (d *Dispatcher) acquire(id string) (*Instance, *acquireErr) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	e, ok := d.ns[id]
	if !ok {
		if d.max > 0 && len(d.ns) >= d.max {
			d.log.Warn().Str("namespace", id).Int("max", d.max).Msg("namespace limit reached")
			return nil, &acquireErr{http.StatusTooManyRequests, "Too Many Requests",
				"namespace limit reached (NAMESPACE_MAX=" + itoa(d.max) + "); delete unused namespaces with DELETE /admin0/namespaces/{id}",
				"too_many_namespaces"}
		}
		inst, err := d.build(id)
		if err != nil {
			d.log.Error().Err(err).Str("namespace", id).Msg("namespace build failed")
			return nil, &acquireErr{http.StatusInternalServerError, "Internal Server Error",
				"building namespace " + quote(id) + " failed: " + err.Error(), "namespace_build_failed"}
		}
		e = &entry{inst: inst, created: now}
		d.ns[id] = e
	}
	e.lastUsed = now
	return e.inst, nil
}

// lifecycle serves the global namespace routes. The named flag says whether the
// request also selected a namespace by header or prefix.
func (d *Dispatcher) lifecycle(w http.ResponseWriter, r *http.Request, path string, named bool) {
	switch {
	case path == listPath && r.Method == http.MethodGet:
		d.list(w)
	case path == listPath && r.Method == http.MethodDelete:
		if named {
			invalid(w, "DELETE /admin0/namespaces deletes every namespace; send it without a namespace header or /ns/ prefix")
			return
		}
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
