// Package seed loads a YAML or JSON seed file into the mock's in-memory
// stores: expectations, claims, claim mappings, permissions and the MFA flag.
package seed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
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

// numberMarker prefixes the text of a number that must reach the JSON output
// exactly as written. YAML input has to be valid UTF-8, so no YAML string can
// start with the byte 0xff and the marker cannot clash with real content.
const numberMarker = "\xff"

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// prepareScalars retags scalars so they keep the exact text written in the
// file. Timestamps become strings instead of being parsed into time.Time and
// re-rendered. Numbers that are valid JSON literals become marked strings, which
// toJSONValue turns into json.Number; decoding them into int or float64 would
// round big or precise values. Mapping keys are only checked for timestamps, as
// they become text anyway.
func prepareScalars(n *yaml.Node) {
	prepareNode(n, false)
}

func prepareNode(n *yaml.Node, isKey bool) {
	if n.Kind == yaml.ScalarNode {
		switch n.ShortTag() {
		case "!!timestamp":
			n.Tag = "!!str"
		case "!!int", "!!float":
			if !isKey && jsonNumber.MatchString(n.Value) {
				n.Tag = "!!str"
				n.Value = numberMarker + n.Value
				n.Style = 0
			}
		}
	}
	for i, c := range n.Content {
		prepareNode(c, n.Kind == yaml.MappingNode && i%2 == 0)
	}
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
	prepareScalars(node)
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

	s := &Seed{
		Claims:        raw.Claims,
		ClaimMappings: raw.ClaimMappings,
		Permissions:   raw.Permissions,
		MFARequired:   raw.MFARequired,
	}
	for i, r := range raw.Expectations {
		prefix := "expectations[" + strconv.Itoa(i) + "]: "
		var item Item
		if err := decodeStrict(r, &item); err != nil {
			return nil, errors.New(prefix + friendly(err))
		}
		exp, err := matches.Prepare(v, item.Registration)
		if err != nil {
			var re *matches.RegistrationError
			if errors.As(err, &re) {
				return nil, errors.New(prefix + re.Message)
			}
			return nil, errors.New(prefix + err.Error())
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
