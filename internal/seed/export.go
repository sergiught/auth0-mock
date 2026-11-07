package seed

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sergiught/auth0-mock/internal/matches"
)

// Export dumps the current state of st in seed file shape. Bodies are
// normalised (sorted keys, exact numbers) so the output is stable. When
// detail is true each expectation also carries its id and hit count.
func Export(st Stores, detail bool) File {
	var f File
	for _, e := range st.Matches.List() {
		item := Item{Registration: matches.Registration{
			Method:   e.Method,
			Path:     e.Path,
			Request:  normaliseRequest(e.Request),
			Response: normaliseResponse(e.Response),
		}}
		for _, t := range e.Then {
			item.Then = append(item.Then, normaliseResponse(t))
		}
		if detail {
			item.ID = e.ID
			hits := e.Hits
			item.Hits = &hits
		}
		f.Expectations = append(f.Expectations, item)
	}
	if c := st.Claims.Get(); len(c) > 0 {
		f.Claims = c
	}
	if m := st.ClaimMappings.Get(); len(m) > 0 {
		f.ClaimMappings = m
	}
	if p := st.Permissions.All(); len(p) > 0 {
		f.Permissions = p
	}
	f.MFARequired = st.MFA.IsRequired()
	return f
}

func normaliseRequest(r *matches.RequestMatcher) *matches.RequestMatcher {
	if r == nil {
		return nil
	}
	c := *r
	c.Body = normaliseBody(c.Body)
	return &c
}

func normaliseResponse(r matches.ResponseDef) matches.ResponseDef {
	r.Body = normaliseBody(r.Body)
	return r
}

// normaliseBody decodes with UseNumber and re-marshals: map keys come out
// sorted and numbers keep their exact text.
func normaliseBody(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return b
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return b
	}
	out, err := marshalNoEscape(v, false)
	if err != nil {
		return b
	}
	return bytes.TrimSuffix(out, []byte("\n"))
}

func marshalNoEscape(v any, indent bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// JSON renders f as indented JSON ending in a newline.
func (f File) JSON() ([]byte, error) {
	return marshalNoEscape(f, true)
}

// YAML renders f as YAML with alphabetical keys and exact numbers.
func (f File) YAML() ([]byte, error) {
	js, err := f.JSON()
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(toYAMLNode(v)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// toYAMLNode builds a YAML tree from a value decoded with UseNumber. Yaml.v3
// would write a json.Number as a quoted string, so numbers become explicit
// !!int or !!float scalars carrying their exact text.
func toYAMLNode(v any) *yaml.Node {
	switch x := v.(type) {
	case map[string]any:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			if k == "<<" {
				key.Style = yaml.DoubleQuotedStyle // A plain << is the merge key.
			}
			n.Content = append(n.Content, key, toYAMLNode(x[k]))
		}
		return n
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range x {
			n.Content = append(n.Content, toYAMLNode(e))
		}
		return n
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(string(x), ".eE") {
			tag = "!!float"
		} else if _, err := strconv.ParseInt(string(x), 10, 64); err != nil {
			if _, err := strconv.ParseUint(string(x), 10, 64); err != nil {
				tag = "!!float" // Too big for !!int, which would not load back.
			}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(x)}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: x}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
}
