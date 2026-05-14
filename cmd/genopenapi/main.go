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
	"sort"
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
	if err := os.WriteFile(out, append(body, '\n'), 0o600); err != nil {
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

	// Prefix every Mgmt API path with the base path derived from servers[0].url
	// (e.g. "/api/v2") so that the merged document carries the full path.
	// Fragment paths (Auth API, admin0, service) are left untouched because
	// those surfaces are mounted at the chi root without any prefix.
	var bp string
	if len(base.Servers) > 0 {
		bp = basePath(base.Servers[0].URL)
		if bp != "" {
			prefixed := openapi3.NewPaths()
			for path, item := range base.Paths.Map() {
				prefixed.Set(bp+path, item)
			}
			base.Paths = prefixed
		}
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
			return nil, fmt.Errorf("fragment %d: %w", i, err)
		}
	}

	synthesiseMockControlSiblings(base, bp)

	// Servers rewrite (kept here so bundle() returns a fully-formed doc).
	base.Servers = openapi3.Servers{{
		URL:         server,
		Description: "Local auth0-mock",
	}}

	rewriteInfo(base)
	applyTagGroups(base)
	return base, nil
}

// applyTagGroups injects the `x-tagGroups` Scalar/Redoc vendor extension so
// the rendered docs sidebar splits into four top-level sections instead of
// the ~50 flat tags the upstream Mgmt API ships. The /match and /reset
// siblings are intentionally NOT in a separate group: they inherit their
// parent's tag (see collectPathItemTags) and therefore appear adjacent to it
// inside the Management API group.
func applyTagGroups(base *openapi3.T) {
	used := map[string]struct{}{}
	if base.Paths != nil {
		for _, item := range base.Paths.Map() {
			for _, op := range item.Operations() {
				if op == nil {
					continue
				}
				for _, t := range op.Tags {
					used[t] = struct{}{}
				}
			}
		}
	}
	const (
		tagAuthAPI = "auth-api"
		tagAdmin0  = "admin0"
		tagService = "service"
	)
	fragment := map[string]struct{}{
		tagAuthAPI: {},
		tagAdmin0:  {},
		tagService: {},
	}
	var mgmtTags []string
	for name := range used {
		if _, isFragment := fragment[name]; isFragment {
			continue
		}
		mgmtTags = append(mgmtTags, name)
	}
	sort.Strings(mgmtTags)

	type tagGroup struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	groups := []tagGroup{
		{Name: "Authentication API", Tags: []string{tagAuthAPI}},
		{Name: "Management API", Tags: mgmtTags},
		{Name: "admin0", Tags: []string{tagAdmin0}},
		{Name: "Service", Tags: []string{tagService}},
	}
	if base.Extensions == nil {
		base.Extensions = map[string]any{}
	}
	base.Extensions["x-tagGroups"] = groups
}

// rewriteInfo replaces the upstream Auth0 Management API's `info` block with
// auth0-mock's own branding so the rendered docs lead with "what this mock
// is" instead of pointing readers at Auth0 Support / Auth0 ToS.
func rewriteInfo(base *openapi3.T) {
	upstreamVersion := "2.0"
	if base.Info != nil && base.Info.Version != "" {
		upstreamVersion = base.Info.Version
	}
	base.Info = &openapi3.Info{
		Title:       "auth0-mock API",
		Description: docsDescription,
		Version:     upstreamVersion,
		Contact: &openapi3.Contact{
			Name: "auth0-mock",
			URL:  "https://github.com/sergiught/auth0-mock",
		},
		License: &openapi3.License{
			Name: "MIT",
			URL:  "https://github.com/sergiught/auth0-mock/blob/main/LICENSE",
		},
	}
	base.ExternalDocs = &openapi3.ExternalDocs{
		Description: "auth0-mock on GitHub",
		URL:         "https://github.com/sergiught/auth0-mock",
	}
}

const docsDescription = "" +
	"**auth0-mock** is a drop-in mock of Auth0's Authentication and Management " +
	"APIs. Run it locally and point your application's `AUTH0_DOMAIN` at it — " +
	"your code calls auth0-mock the same way it calls Auth0, with real signed " +
	"JWTs verifiable against the JWKS published at `/.well-known/jwks.json`.\n\n" +
	"This document covers every HTTP surface the mock exposes:\n\n" +
	"- **Authentication API** — `/oauth/token`, `/authorize`, `/userinfo`, " +
	"`/v2/logout`, `/dbconnections/*`, `/passwordless/*`.\n" +
	"- **Management API** — every endpoint under `/api/v2` from the upstream " +
	"Auth0 spec, plus a `POST {path}/match` and `POST {path}/reset` sibling " +
	"per operation so you can programme canned responses from this same page.\n" +
	"- **admin0** — control plane under `/admin0/*` for direct manipulation " +
	"of in-memory state (registered matches, claim overlay, per-audience " +
	"permissions, MFA-required flag).\n" +
	"- **service** — `/healthz`, `/.well-known/jwks.json`, `/openapi.json`, " +
	"`/openapi.yaml`, `/docs`.\n\n" +
	"The **Try it** panel on this page is preloaded with a freshly-minted " +
	"`client_credentials` token, so Management API calls succeed without any " +
	"manual setup. The token's audience defaults to `DEFAULT_AUDIENCE` " +
	"(`https://localhost:8443/api/v2/`) — override per-request from the auth " +
	"selector if your test needs a different one.\n\n" +
	"Project source: <https://github.com/sergiught/auth0-mock>"

// mergeFragment copies frag.paths, frag.components and frag.tags into base,
// returning an error if any path+method, schema name, or tag name is declared
// twice.
func mergeFragment(base, frag *openapi3.T) error {
	if err := mergePaths(base, frag); err != nil {
		return err
	}
	if err := mergeSchemas(base, frag); err != nil {
		return err
	}
	if err := mergeSecuritySchemes(base, frag); err != nil {
		return err
	}
	return mergeTags(base, frag)
}

// mergeTags copies frag.Tags into base.Tags so the merged document carries a
// top-level tag entry (with description) for every surface — Scalar uses these
// to render sidebar section headers instead of bare tag names.
func mergeTags(base, frag *openapi3.T) error {
	if len(frag.Tags) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, t := range base.Tags {
		if t != nil {
			seen[t.Name] = struct{}{}
		}
	}
	for _, t := range frag.Tags {
		if t == nil {
			continue
		}
		if _, dup := seen[t.Name]; dup {
			return fmt.Errorf("conflict: tag %q declared in both base and fragment", t.Name)
		}
		base.Tags = append(base.Tags, t)
		seen[t.Name] = struct{}{}
	}
	return nil
}

func mergePaths(base, frag *openapi3.T) error {
	if frag.Paths == nil {
		return nil
	}
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
	return nil
}

func mergeSchemas(base, frag *openapi3.T) error {
	if frag.Components == nil || frag.Components.Schemas == nil {
		return nil
	}
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
	return nil
}

func mergeSecuritySchemes(base, frag *openapi3.T) error {
	if frag.Components == nil || frag.Components.SecuritySchemes == nil {
		return nil
	}
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
	return nil
}

// securitySchemesEqual reports whether two security-scheme references describe
// the same authentication shape. We compare the load-bearing fields directly
// (case-insensitively, because `bearerFormat` is "jwt" in the upstream Mgmt
// API spec but "JWT" in the authapi fragment) rather than round-tripping
// through JSON — the latter is sensitive to map ordering and would treat
// trivial description edits as conflicts.
func securitySchemesEqual(a, b *openapi3.SecuritySchemeRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	av, bv := a.Value, b.Value
	if av == nil || bv == nil {
		return av == bv
	}
	return strings.EqualFold(av.Type, bv.Type) &&
		strings.EqualFold(av.Scheme, bv.Scheme) &&
		strings.EqualFold(av.BearerFormat, bv.BearerFormat)
}

// basePath extracts the path component of a server URL, e.g.
// "https://{tenantDomain}/api/v2" => "/api/v2". Copied from
// internal/spec.deriveBasePath (unexported) to avoid coupling.
func basePath(url string) string {
	for i := 0; i+2 < len(url); i++ {
		if url[i] == ':' && url[i+1] == '/' && url[i+2] == '/' {
			j := i + 3
			for j < len(url) && url[j] != '/' {
				j++
			}
			return url[j:]
		}
	}
	return url
}

// synthesiseMockControlSiblings adds POST {path}/match and POST {path}/reset
// for every Management API path in base.Paths, skipping siblings whose
// path+method would collide with a real operation already in the spec.
func synthesiseMockControlSiblings(base *openapi3.T, mgmtPrefix string) {
	if base.Paths == nil {
		return
	}
	// Snapshot the existing paths (we mutate base.Paths as we go).
	existing := map[string]*openapi3.PathItem{}
	for p, item := range base.Paths.Map() {
		existing[p] = item
	}
	for path, parentItem := range existing {
		if mgmtPrefix == "" || !strings.HasPrefix(path, mgmtPrefix+"/") {
			continue
		}
		parentTags := collectPathItemTags(parentItem)
		for _, suffix := range []string{"/match", "/reset"} {
			siblingPath := path + suffix
			if base.Paths.Value(siblingPath) != nil {
				// Real spec operation already lives here (e.g.
				// /branding/phone/templates/{id}/reset) — leave it alone.
				continue
			}
			sibling := &openapi3.PathItem{}
			sibling.SetOperation("POST", mockControlOperation(suffix, parentTags))
			base.Paths.Set(siblingPath, sibling)
		}
	}
}

// collectPathItemTags returns the union of tags across every operation on a
// path item, sorted for stable output. Used so /match and /reset siblings
// inherit their parent endpoint's tag and therefore appear grouped with it in
// the rendered docs instead of in a separate mock-control bucket.
func collectPathItemTags(item *openapi3.PathItem) []string {
	if item == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, op := range item.Operations() {
		if op == nil {
			continue
		}
		for _, t := range op.Tags {
			seen[t] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// mockControlOperation builds the synthesised OpenAPI operation for /match or
// /reset siblings. Bodies reference the shared schemas in MockControlOpenAPIYAML.
// ParentTags is the deduplicated tag set lifted from the parent endpoint so the
// sibling renders adjacent to it in the docs.
func mockControlOperation(suffix string, parentTags []string) *openapi3.Operation {
	op := &openapi3.Operation{
		Tags:        parentTags,
		Summary:     summaryFor(suffix),
		Description: descriptionFor(suffix),
		Responses:   openapi3.NewResponses(),
	}
	switch suffix {
	case "/match":
		op.RequestBody = &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: true,
				Content: openapi3.NewContentWithJSONSchemaRef(&openapi3.SchemaRef{
					Ref: "#/components/schemas/MatchRegistration",
				}),
			},
		}
		op.Responses.Set("204", &openapi3.ResponseRef{
			Value: &openapi3.Response{
				Description: ptr("Registration stored. Subsequent matching requests will receive the programmed response."),
			},
		})
	case "/reset":
		op.Responses.Set("204", &openapi3.ResponseRef{
			Value: &openapi3.Response{
				Description: ptr("Programmed responses cleared for the paired operation."),
			},
		})
	}
	op.Responses.Set("400", &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: ptr("Body could not be parsed or violated the response schema for the paired operation."),
		},
	})
	return op
}

func summaryFor(suffix string) string {
	switch suffix {
	case "/match":
		return "Programme the next canned response for the paired operation."
	case "/reset":
		return "Clear programmed responses for the paired operation."
	}
	return ""
}

func descriptionFor(suffix string) string {
	switch suffix {
	case "/match":
		return "Send a `MatchRegistration` body. The mock validates `body` against the paired operation's response schema for the given `status` before storing."
	case "/reset":
		return "No request body. Clears any registered match for the paired operation."
	}
	return ""
}

func ptr[T any](v T) *T { return &v }
