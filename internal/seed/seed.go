// Package seed loads a YAML or JSON seed file into the mock's in-memory
// stores: expectations, claims, claim mappings, permissions and the MFA flag.
package seed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Stores are the stores a seed is applied to.
type Stores struct {
	Matches       *matches.Store
	Claims        *claims.Store
	ClaimMappings *claims.MappingStore
	Permissions   *permissions.Store
	MFA           *mfa.Store
}

// File is the on-disk shape of a seed file, shared with the export.
type File struct {
	Expectations  []Item              `json:"expectations,omitempty"`
	Claims        map[string]any      `json:"claims,omitempty"`
	ClaimMappings map[string]string   `json:"claim_mappings,omitempty"`
	Permissions   map[string][]string `json:"permissions,omitempty"`
	MFARequired   bool                `json:"mfa_required,omitempty"`
}

// Item is one expectation entry. ID and Hits appear in exports; they are
// accepted on load and ignored.
type Item struct {
	ID string `json:"id,omitempty"`
	matches.Registration
	Hits *int64 `json:"hits,omitempty"`
}

// Seed is a parsed, validated seed file.
type Seed struct {
	Expectations  []matches.Expectation
	Claims        map[string]any
	ClaimMappings map[string]string
	Permissions   map[string][]string
	MFARequired   bool
}

// rawFile mirrors File but keeps items raw so each can be decoded strictly
// and reported by index.
type rawFile struct {
	Expectations  []json.RawMessage   `json:"expectations"`
	Claims        map[string]any      `json:"claims"`
	ClaimMappings map[string]string   `json:"claim_mappings"`
	Permissions   map[string][]string `json:"permissions"`
	MFARequired   bool                `json:"mfa_required"`
}

var unknownField = regexp.MustCompile(`json: unknown field "([^"]*)"`)

func friendly(err error) string {
	return unknownField.ReplaceAllString(err.Error(), `unknown key "$1"`)
}

func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

// checkKeyCase rejects a key that encoding/json matched to a field of t only
// because it ignores case, such as "Method" for "method". Without it a file
// with both "expectations" and "Expectations" would silently keep one list.
// Free-form maps (claims, headers, bodies) keep whatever case they have.
func checkKeyCase(v any, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, _ := v.(map[string]any)
		fields := jsonFields(t)
		for _, k := range slices.Sorted(maps.Keys(m)) {
			ft, ok := fields[k]
			if !ok {
				return fmt.Errorf("unknown key %q (keys are case-sensitive)", k)
			}
			if err := checkKeyCase(m[k], ft); err != nil {
				return err
			}
		}
	case reflect.Slice:
		l, _ := v.([]any)
		for _, e := range l {
			if err := checkKeyCase(e, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		m, _ := v.(map[string]any)
		for _, k := range slices.Sorted(maps.Keys(m)) {
			if err := checkKeyCase(m[k], t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// jsonFields maps each JSON key of struct type t, including keys of embedded
// structs, to its field type.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" {
			maps.Copy(out, jsonFields(f.Type))
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if name != "-" {
			out[name] = f.Type
		}
	}
	return out
}

// numberMarker prefixes the text of a number that must reach the JSON output
// exactly as written. YAML input has to be valid UTF-8, so no YAML string can
// start with the byte 0xff and the marker cannot clash with real content.
const numberMarker = "\xff"

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// prepareScalars retags scalars so they keep the exact text written in the
// file. Timestamps become strings instead of being parsed into time.Time and
// re-rendered. Numbers that are valid JSON literals become marked strings, which
// toJSONValue turns into json.Number; decoding them into int or float64 would
// round big or precise values. Keys are marked too: toJSONValue drops the marker
// from a key, so it keeps its written text, and an alias of an anchored key
// still gets the exact number. It also rejects two keys of one mapping that
// become the same text, such as 0x1F and 0o37; the YAML decoder would
// otherwise keep only the second one.
func prepareScalars(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		switch n.ShortTag() {
		case "!!timestamp":
			n.Tag = "!!str"
		case "!!int", "!!float":
			if jsonNumber.MatchString(n.Value) {
				n.Tag = "!!str"
				n.Value = numberMarker + n.Value
				n.Style = 0
			}
		}
	}
	for _, c := range n.Content {
		if err := prepareScalars(c); err != nil {
			return err
		}
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		var k any
		if err := n.Content[i].Decode(&k); err != nil {
			return err
		}
		switch k.(type) {
		case map[string]any, map[any]any, []any:
			continue // Not a scalar: toJSONValue rejects it.
		}
		text := keyText(k)
		if seen[text] {
			return fmt.Errorf("duplicate key %q after conversion to text", text)
		}
		seen[text] = true
	}
	return nil
}

// decodeSingleDocument parses data as exactly one YAML document. It returns a
// nil node for empty input.
func decodeSingleDocument(data []byte) (*yaml.Node, error) {
	var node yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&node); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil //nolint:nilnil // nil means an empty file
		}
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("seed file must contain a single YAML document")
	}
	return &node, nil
}

// Parse reads YAML (or JSON, which is a subset) and validates every
// expectation against the Management API spec.
func Parse(data []byte, v *spec.Validator) (*Seed, error) {
	node, err := decodeSingleDocument(data)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return &Seed{}, nil
	}
	if err := prepareScalars(node); err != nil {
		return nil, err
	}
	var doc any
	if err := node.Decode(&doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return &Seed{}, nil
	}
	switch doc.(type) {
	case map[string]any, map[any]any:
	default:
		return nil, errors.New("the seed file must be a mapping")
	}
	conv, err := toJSONValue(doc)
	if err != nil {
		return nil, err
	}
	js, err := json.Marshal(conv)
	if err != nil {
		return nil, err
	}

	var raw rawFile
	if err := decodeStrict(js, &raw); err != nil {
		return nil, errors.New(friendly(err))
	}
	if err := checkKeyCase(conv, reflect.TypeFor[rawFile]()); err != nil {
		return nil, err
	}
	items, _ := conv.(map[string]any)["expectations"].([]any)

	s := &Seed{
		Claims:        raw.Claims,
		ClaimMappings: raw.ClaimMappings,
		Permissions:   raw.Permissions,
		MFARequired:   raw.MFARequired,
	}
	for i, r := range raw.Expectations {
		exp, err := parseItem(r, items[i], v)
		if err != nil {
			return nil, errors.New("expectations[" + strconv.Itoa(i) + "]: " + err.Error())
		}
		for j, prev := range s.Expectations {
			if matches.SameTarget(prev, exp) {
				return nil, fmt.Errorf("expectations[%d] and expectations[%d] have the same method, path and request matcher", j, i)
			}
		}
		s.Expectations = append(s.Expectations, exp)
	}
	return s, nil
}

// parseItem decodes and validates one expectation. The generic argument is the
// same item as decoded from the file, used to check key case.
func parseItem(r json.RawMessage, generic any, v *spec.Validator) (matches.Expectation, error) {
	var item Item
	if err := decodeStrict(r, &item); err != nil {
		return matches.Expectation{}, errors.New(friendly(err))
	}
	if err := checkKeyCase(generic, reflect.TypeFor[Item]()); err != nil {
		return matches.Expectation{}, err
	}
	exp, err := matches.Prepare(v, item.Registration)
	var re *matches.RegistrationError
	if errors.As(err, &re) {
		return matches.Expectation{}, errors.New(re.Message)
	}
	return exp, err
}

// Apply writes the seed into the stores. Expectations are Put in file order.
func (s *Seed) Apply(st Stores) {
	for _, e := range s.Expectations {
		st.Matches.Put(e)
	}
	if len(s.Claims) > 0 {
		st.Claims.Set(s.Claims)
	}
	if len(s.ClaimMappings) > 0 {
		st.ClaimMappings.Set(s.ClaimMappings)
	}
	for aud, perms := range s.Permissions {
		st.Permissions.Set(aud, perms)
	}
	if s.MFARequired {
		st.MFA.SetRequired(true)
	}
}

// Summary is a one-line description for logs.
func (s *Seed) Summary() string {
	var parts []string
	add := func(n int, one, many string) {
		switch n {
		case 0:
		case 1:
			parts = append(parts, "1 "+one)
		default:
			parts = append(parts, strconv.Itoa(n)+" "+many)
		}
	}
	add(len(s.Expectations), "expectation", "expectations")
	add(len(s.Claims), "claim", "claims")
	add(len(s.ClaimMappings), "claim mapping", "claim mappings")
	add(len(s.Permissions), "permission audience", "permission audiences")
	if s.MFARequired {
		parts = append(parts, "mfa required")
	}
	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, ", ")
}
