// Package tmpl parses and renders response templates: JSON bodies and header
// values that hold {{uuid}}, {{now}}, {{path.NAME}} and {{body.PATH}}
// placeholders.
package tmpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const nowLayout = "2006-01-02T15:04:05.000Z07:00"

// Vars are the per-call inputs. Body is the raw request body (may be nil or non-JSON).
type Vars struct {
	UUID string
	Now  time.Time
	Path map[string]string
	Body []byte
	// Sample marks a registration-time render: a whole-string body
	// placeholder with no value renders "" in place instead of being dropped.
	Sample bool
}

type segKind int

const (
	segText segKind = iota
	segUUID
	segNow
	segPath
	segBody
)

// seg is one piece of a parsed string: literal text or a placeholder.
type seg struct {
	kind segKind
	text string // Literal text, path name or gjson path.
}

// str is a parsed string value that holds placeholders or escapes.
type str struct{ segs []seg }

// Template is a parsed response template. It is immutable after Parse, so
// Render is safe for concurrent use.
type Template struct {
	body     any // Decoded JSON tree; templated strings are *str. Nil when empty.
	hasBody  bool
	headers  map[string]*str
	pointers []string // Whole-string {{body.*}} values: any JSON type when served.
	texts    []text   // Other templated strings: always a string when served.
}

// text records a templated string value that renders as text: whether it
// embeds a {{body.*}} placeholder, and the path parameters it names.
type text struct {
	ptr   string
	body  bool
	paths []string
}

// Parse checks body (JSON, may be empty) and header values. The pathNames are the
// parameter names in the operation's template; {{path.X}} with X not in it is an error.
func Parse(body json.RawMessage, headers map[string]string, pathNames []string) (*Template, error) {
	t := &Template{}
	names := make(map[string]bool, len(pathNames))
	for _, n := range pathNames {
		names[n] = true
	}

	if len(bytes.TrimSpace(body)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		var tree any
		if err := dec.Decode(&tree); err != nil {
			return nil, fmt.Errorf("body is not valid JSON: %w", err)
		}
		if _, err := dec.Token(); err != io.EOF {
			return nil, fmt.Errorf("body is not valid JSON: trailing data")
		}
		walked, err := t.walk(tree, "", names)
		if err != nil {
			return nil, err
		}
		t.body, t.hasBody = walked, true
		sort.Strings(t.pointers)
	}

	if len(headers) > 0 {
		t.headers = make(map[string]*str, len(headers))
		for k, v := range headers {
			s, err := parseString(v, names)
			if err != nil {
				return nil, fmt.Errorf("header %q: %w", k, err)
			}
			t.headers[k] = s
		}
	}
	return t, nil
}

// walk replaces every string that needs rendering with a *str.
func (t *Template) walk(v any, ptr string, names map[string]bool) (any, error) {
	switch x := v.(type) {
	case string:
		return t.walkString(x, ptr, names)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			w, err := t.walk(e, ptr+"/"+escapePointer(k), names)
			if err != nil {
				return nil, err
			}
			out[k] = w
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			w, err := t.walk(e, fmt.Sprintf("%s/%d", ptr, i), names)
			if err != nil {
				return nil, err
			}
			out[i] = w
		}
		return out, nil
	default:
		return v, nil
	}
}

// walkString parses one string value, recording its pointer when its
// rendered form depends on the request.
func (t *Template) walkString(x, ptr string, names map[string]bool) (any, error) {
	if !strings.Contains(x, "{{") {
		return x, nil
	}
	s, err := parseString(x, names)
	if err != nil {
		return nil, err
	}
	if len(s.segs) == 1 && s.segs[0].kind == segBody {
		t.pointers = append(t.pointers, ptr)
		return s, nil
	}
	tx := text{ptr: ptr}
	for _, sg := range s.segs {
		switch sg.kind {
		case segBody:
			tx.body = true
		case segPath:
			tx.paths = append(tx.paths, sg.text)
		}
	}
	if tx.body || len(tx.paths) > 0 {
		t.texts = append(t.texts, tx)
	}
	return s, nil
}

func escapePointer(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

func parseString(s string, names map[string]bool) (*str, error) {
	var segs []seg
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, seg{kind: segText, text: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], `\{{`):
			lit.WriteString("{{")
			i += 3
		case strings.HasPrefix(s[i:], "{{"):
			end := placeholderEnd(s[i:])
			if end < 0 {
				return nil, fmt.Errorf("unclosed placeholder %q", s[i:])
			}
			raw := s[i : i+end+2]
			sg, err := parsePlaceholder(strings.TrimSpace(raw[2:len(raw)-2]), names)
			if err != nil {
				return nil, fmt.Errorf("invalid placeholder %q: %w", raw, err)
			}
			flush()
			segs = append(segs, sg)
			i += end + 2
		default:
			lit.WriteByte(s[i])
			i++
		}
	}
	flush()
	return &str{segs: segs}, nil
}

// placeholderEnd returns the index of the "}}" closing the placeholder that
// opens at the start of s, or -1. Braces in the name are balanced first, so a
// gjson multipath like {{body.{a,b}}} ends at its last two braces, and braces
// inside a quoted path key are ignored.
func placeholderEnd(s string) int {
	depth := 0
	quoted := false
	for j := 2; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '\\':
			j++ // The next byte is escaped, so it is part of the name.
		case quoted:
			quoted = c != '"'
		case c == '"':
			quoted = true
		case c == '{':
			depth++
		case depth == 0 && strings.HasPrefix(s[j:], "}}"):
			return j
		case c == '}' && depth > 0:
			depth--
		}
	}
	return -1
}

func parsePlaceholder(name string, names map[string]bool) (seg, error) {
	switch {
	case name == "uuid":
		return seg{kind: segUUID}, nil
	case name == "now":
		return seg{kind: segNow}, nil
	case strings.HasPrefix(name, "path."):
		p := strings.TrimPrefix(name, "path.")
		if !names[p] {
			return seg{}, fmt.Errorf("no path parameter %q", p)
		}
		return seg{kind: segPath, text: p}, nil
	case strings.HasPrefix(name, "body."):
		p := strings.TrimPrefix(name, "body.")
		if p == "" {
			return seg{}, fmt.Errorf("empty body path")
		}
		return seg{kind: segBody, text: p}, nil
	}
	return seg{}, fmt.Errorf("unknown placeholder name %q", name)
}

// Render returns the rendered body (nil when the template body was empty) and headers.
func (t *Template) Render(v Vars) (body []byte, headers map[string]string, err error) {
	r := renderer{v: v, valid: gjson.ValidBytes(v.Body)}

	if len(t.headers) > 0 {
		headers = make(map[string]string, len(t.headers))
		for k, s := range t.headers {
			val := r.text(s)
			if strings.ContainsAny(val, "\r\n") {
				return nil, nil, fmt.Errorf("header %q renders with a line break", k)
			}
			headers[k] = val
		}
	}

	if !t.hasBody {
		return nil, headers, nil
	}
	// A dropped top-level value is already "", so the flag is not needed.
	rendered, _ := r.render(t.body)
	// Marshal escapes <, > and & as \u003c and friends. Request-derived
	// values land in this body, so they must never come out as literal HTML.
	out, err := json.Marshal(rendered)
	if err != nil {
		return nil, nil, fmt.Errorf("encode rendered body: %w", err)
	}
	return out, headers, nil
}

type renderer struct {
	v     Vars
	valid bool
}

// render rebuilds the tree with placeholders replaced, leaving the parsed tree untouched.
// The bool is false when n is a whole-string body placeholder with no value:
// the caller drops it from its parent object or array.
func (r renderer) render(n any) (any, bool) {
	switch x := n.(type) {
	case *str:
		if len(x.segs) == 1 && x.segs[0].kind == segBody {
			if res := r.lookup(x.segs[0].text); res.Exists() {
				return json.RawMessage(res.Raw), true
			}
			return "", r.v.Sample
		}
		return r.text(x), true
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if v, ok := r.render(e); ok {
				out[k] = v
			}
		}
		return out, true
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			if v, ok := r.render(e); ok {
				out = append(out, v)
			}
		}
		return out, true
	default:
		return n, true
	}
}

func (r renderer) lookup(path string) gjson.Result {
	if !r.valid {
		return gjson.Result{}
	}
	return gjson.GetBytes(r.v.Body, path)
}

// text renders s as plain text, using the embedded rule for body placeholders.
func (r renderer) text(s *str) string {
	var b strings.Builder
	for _, sg := range s.segs {
		switch sg.kind {
		case segText:
			b.WriteString(sg.text)
		case segUUID:
			b.WriteString(r.v.UUID)
		case segNow:
			b.WriteString(r.v.Now.UTC().Format(nowLayout))
		case segPath:
			b.WriteString(r.v.Path[sg.text])
		case segBody:
			res := r.lookup(sg.text)
			switch {
			case !res.Exists():
			case res.Type == gjson.String:
				b.WriteString(res.String())
			default:
				b.WriteString(res.Raw)
			}
		}
	}
	return b.String()
}

// BodyPointers returns the JSON pointers ("/a/0/b") of every string value
// that is a single {{body.*}} placeholder, sorted. Such a value takes the
// type of the request field when served.
func (t *Template) BodyPointers() []string {
	return append([]string(nil), t.pointers...)
}

// TextPointers returns the JSON pointers, sorted, of every other templated
// string value whose text depends on the request: it embeds a {{body.*}}
// placeholder or names a path parameter in unresolved. Such a value is
// always a string, but its text is only known when served.
func (t *Template) TextPointers(unresolved []string) []string {
	var out []string
	for _, tx := range t.texts {
		if tx.body || slices.ContainsFunc(tx.paths, func(p string) bool { return slices.Contains(unresolved, p) }) {
			out = append(out, tx.ptr)
		}
	}
	sort.Strings(out)
	return out
}

// PathNames returns the {name} segments of an OpenAPI path template, in order.
func PathNames(template string) []string {
	var names []string
	for _, s := range strings.Split(template, "/") {
		if len(s) > 2 && s[0] == '{' && s[len(s)-1] == '}' {
			names = append(names, s[1:len(s)-1])
		}
	}
	return names
}

// PathValues matches an escaped request path against template segment by
// segment, path-unescaping each matched value. Returns nil when the segment counts differ.
func PathValues(template, escapedPath string) map[string]string {
	ts := strings.Split(template, "/")
	ps := strings.Split(escapedPath, "/")
	if len(ts) != len(ps) {
		return nil
	}
	out := map[string]string{}
	for i, s := range ts {
		if len(s) > 2 && s[0] == '{' && s[len(s)-1] == '}' {
			val, err := url.PathUnescape(ps[i])
			if err != nil {
				val = ps[i]
			}
			out[s[1:len(s)-1]] = val
		}
	}
	return out
}
