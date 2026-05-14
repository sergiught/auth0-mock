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
// the ~50 flat tags the upstream Mgmt API ships. Authentication API and
// admin0 carry several sub-tags each (declared in their fragments) so the
// group→tag nesting is meaningful rather than redundant. The /match and
// /reset siblings are intentionally NOT in a separate group: each inherits
// the tag of the parent operation it pairs with (see
// synthesiseMockControlSiblings) and so appears adjacent to it inside the
// Management API group.
//
// Note: with x-tagGroups, a tag that is in no group is dropped from the
// sidebar entirely. Management API is computed as "every used tag not claimed
// by a surface fragment", so a tag can never go ungrouped — at worst a new,
// unlisted fragment tag is miscategorised into Management API (visible, just
// in the wrong section). AuthAPITags/admin0Tags/serviceTags must stay in sync
// with the fragment YAML; TestBundleAppliesTagGroupsForSidebar guards that.
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
	// Tags contributed by each non-Mgmt surface fragment. Everything else is a
	// real Auth0 Management API tag and falls into the Management API group.
	authAPITags := []string{"OAuth & OIDC", "Database Connections", "Passwordless"}
	admin0Tags := []string{"Claims", "Permissions", "MFA", "Matches"}
	serviceTags := []string{"Service"}

	fragment := map[string]struct{}{}
	for _, list := range [][]string{authAPITags, admin0Tags, serviceTags} {
		for _, t := range list {
			fragment[t] = struct{}{}
		}
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
		{Name: "Authentication API", Tags: authAPITags},
		{Name: "Management API", Tags: mgmtTags},
		{Name: "admin0", Tags: admin0Tags},
		{Name: "Service", Tags: serviceTags},
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
	"Auth0 spec, plus a `{path}/match` and `{path}/reset` sibling per operation " +
	"so you can programme canned responses from this same page. Each sibling " +
	"uses the same HTTP method as the operation it pairs with — `GET {path}/match` " +
	"programmes the GET, `POST {path}/match` programmes the POST.\n" +
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

// synthesiseMockControlSiblings adds {path}/match and {path}/reset operations
// for every Management API operation in base.Paths.
//
// The siblings are method-aware: the running mock (see
// internal/mgmtapi/mount.go) registers one sibling per parent operation, on
// that operation's own method — so GET /api/v2/actions/actions and
// POST /api/v2/actions/actions each get their own GET .../match and
// POST .../match. A sibling method is skipped only when a real spec operation
// already occupies that exact method+path (e.g. the real
// PATCH /branding/phone/templates/{id}/reset).
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
		for _, suffix := range []string{"/match", "/reset"} {
			siblingPath := path + suffix
			// Reuse the path item if a real spec operation already lives here
			// (e.g. the real PATCH /branding/phone/templates/{id}/reset) so we
			// only add the non-colliding methods; otherwise start fresh.
			sibling := base.Paths.Value(siblingPath)
			if sibling == nil {
				sibling = &openapi3.PathItem{}
			}
			for method, parentOp := range parentItem.Operations() {
				if sibling.GetOperation(method) != nil {
					// Real spec operation occupies this method+path — the
					// running mock skips the sibling here too.
					continue
				}
				sibling.SetOperation(method,
					mockControlOperation(suffix, method, path, parentOp.Tags))
			}
			base.Paths.Set(siblingPath, sibling)
		}
	}
}

// mockControlOperation builds the synthesised OpenAPI operation for a /match or
// /reset sibling. Bodies reference the shared schemas in MockControlOpenAPIYAML.
// ParentTags is lifted from the paired parent operation so the sibling renders
// under the same tag; method and parentPath identify the exact parent
// operation, and are woven into the operationId and summary so every sibling
// is a distinct, navigable sidebar entry (e.g. GET vs POST .../match on the
// same path get separate rows).
func mockControlOperation(suffix, method, parentPath string, parentTags []string) *openapi3.Operation {
	op := &openapi3.Operation{
		Tags:        parentTags,
		OperationID: operationIDFor(suffix, method, parentPath),
		Summary:     summaryFor(suffix, method, parentPath),
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

// operationIDFor builds a unique, stable operationId for a synthesised sibling
// from its kind ("match"/"reset"), the parent operation's HTTP method, and the
// parent path, e.g. "mock-control.match.get.api.v2.actions.actions". The method
// is part of the id because a single path can host several parent operations
// (GET + POST), each of which gets its own same-method sibling. Path parameter
// braces are stripped and slashes become dots so the id is a clean slug.
func operationIDFor(suffix, method, parentPath string) string {
	kind := strings.TrimPrefix(suffix, "/")
	slug := strings.NewReplacer("/", ".", "{", "", "}", "").
		Replace(strings.TrimPrefix(parentPath, "/"))
	return "mock-control." + kind + "." + strings.ToLower(method) + "." + slug
}

// summaryFor returns the sidebar label for a synthesised sibling. It embeds the
// parent operation's method and path so every entry is distinct and visibly
// tied to the operation it programmes, e.g.
// "match · POST /api/v2/actions/actions".
func summaryFor(suffix, method, parentPath string) string {
	switch suffix {
	case "/match":
		return "match · " + method + " " + parentPath
	case "/reset":
		return "reset · " + method + " " + parentPath
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
