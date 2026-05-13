// Command genopenapi bundles the upstream Auth0 Management API spec, the
// per-package OpenAPI fragments shipped by each surface (authapi, admin0,
// router service endpoints), and synthesised /match + /reset siblings into a
// single OpenAPI 3.1 document.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/authapi"
	"github.com/sergiught/auth0-mock/internal/router"
)

func main() {
	out := flag.String("out", "api/auth0-mock.openapi.json", "output path for the merged OpenAPI JSON")
	server := flag.String("server", "http://localhost:8080", "value for servers[0].url in the merged doc")
	flag.Parse()

	if err := run(*out, *server); err != nil {
		fmt.Fprintln(os.Stderr, "genopenapi:", err)
		os.Exit(1)
	}
}

func run(out, server string) error {
	doc, err := bundle(server)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// bundle stitches together the base Mgmt API spec and the per-package
// fragments shipped by the surface packages, using `server` as the override
// for servers[0].url in the merged doc.
func bundle(server string) (*openapi3.T, error) {
	return bundleWithExtras(server, nil)
}

// bundleWithExtras is the bundle entrypoint used by tests; `extras` are extra
// fragment byte slices appended after the canonical surface fragments.
func bundleWithExtras(server string, extras [][]byte) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false

	base, err := loader.LoadFromData(api.ManagementOpenAPIJSON)
	if err != nil {
		return nil, fmt.Errorf("load base: %w", err)
	}

	fragments := [][]byte{
		api.MockControlOpenAPIYAML,
		router.ServiceFragment,
		authapi.Fragment,
		admin0.Fragment,
	}
	fragments = append(fragments, extras...)

	for i, raw := range fragments {
		frag, err := loader.LoadFromData(raw)
		if err != nil {
			return nil, fmt.Errorf("load fragment %d: %w", i, err)
		}
		if err := mergeFragment(base, frag); err != nil {
			return nil, err
		}
	}

	// Servers rewrite (kept here so bundle() returns a fully-formed doc).
	base.Servers = openapi3.Servers{{
		URL:         server,
		Description: "Local auth0-mock",
	}}
	return base, nil
}

// mergeFragment copies frag.paths and frag.components.schemas into base,
// returning an error if any path+method or schema name is declared twice.
func mergeFragment(base, frag *openapi3.T) error {
	if frag.Paths != nil {
		for path, item := range frag.Paths.Map() {
			existing := base.Paths.Value(path)
			if existing == nil {
				base.Paths.Set(path, item)
				continue
			}
			// Path exists; merge operations method by method, refusing
			// duplicate methods on the same path.
			for method, op := range item.Operations() {
				if existing.GetOperation(method) != nil {
					return fmt.Errorf("conflict: %s %s declared in both base and fragment", method, path)
				}
				existing.SetOperation(method, op)
			}
		}
	}
	if frag.Components != nil && frag.Components.Schemas != nil {
		if base.Components == nil {
			base.Components = &openapi3.Components{}
		}
		if base.Components.Schemas == nil {
			base.Components.Schemas = openapi3.Schemas{}
		}
		for name, schema := range frag.Components.Schemas {
			if _, dup := base.Components.Schemas[name]; dup {
				return fmt.Errorf("conflict: schema %q declared in both base and fragment", name)
			}
			base.Components.Schemas[name] = schema
		}
	}
	if frag.Components != nil && frag.Components.SecuritySchemes != nil {
		if base.Components == nil {
			base.Components = &openapi3.Components{}
		}
		if base.Components.SecuritySchemes == nil {
			base.Components.SecuritySchemes = openapi3.SecuritySchemes{}
		}
		for name, scheme := range frag.Components.SecuritySchemes {
			// Security schemes are routinely re-declared (e.g. bearerAuth);
			// skip when the existing definition is byte-identical, error
			// otherwise.
			if existing, dup := base.Components.SecuritySchemes[name]; dup {
				if !securitySchemesEqual(existing, scheme) {
					return fmt.Errorf("conflict: security scheme %q redefined with different shape", name)
				}
				continue
			}
			base.Components.SecuritySchemes[name] = scheme
		}
	}
	return nil
}

func securitySchemesEqual(a, b *openapi3.SecuritySchemeRef) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	// Normalise to lower-case before comparing so that bearerFormat "JWT" and
	// "jwt" are treated as the same definition.
	return strings.EqualFold(string(ja), string(jb))
}
