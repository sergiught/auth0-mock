// Command genopenapi bundles the embedded Auth0 Management API skeleton, the
// per-package OpenAPI fragments shipped by each surface (authapi, admin0,
// router service endpoints) into a single OpenAPI 3.1 document. With
// -strip-raw it instead runs the vendoring step that produces the skeleton
// from a manually-downloaded raw Auth0 spec.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/authapi"
	"github.com/sergiught/auth0-mock/internal/router"
)

func main() {
	out := flag.String("out", "api/auth0-mock.openapi.json", "output path for the merged OpenAPI JSON")
	server := flag.String("server", "http://localhost:8080", "value for servers[0].url in the merged doc")
	stripRaw := flag.String("strip-raw", "", "if set: read this raw Auth0 spec, strip Auth0's prose, write the skeleton to -out, and exit (the `make refresh-spec` vendoring step)")
	flag.Parse()

	var err error
	if *stripRaw != "" {
		err = runStrip(*stripRaw, *out)
	} else {
		err = run(*out, *server)
	}
	if err != nil {
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

// runStrip loads a raw, manually-downloaded Auth0 Management API spec, removes
// Auth0's authored prose (see stripUpstreamProse), and writes the structural
// skeleton to out. This is the vendoring step behind `make refresh-spec`: the
// raw download is never committed — only the skeleton is. Keeping it a
// deliberate, manual step (rather than a build-time fetch) is intentional, so
// nothing in the project scrapes Auth0's site.
func runStrip(rawPath, out string) error {
	raw, err := os.ReadFile(rawPath) //nolint:gosec // rawPath is a -strip-raw CLI flag supplied by a developer running the vendoring step, not untrusted input
	if err != nil {
		return fmt.Errorf("read raw spec: %w", err)
	}
	doc, err := openapi3.NewLoader().LoadFromData(raw)
	if err != nil {
		return fmt.Errorf("parse raw spec: %w", err)
	}
	stripUpstreamProse(doc)
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skeleton: %w", err)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o600); err != nil { //nolint:gosec // out is the -out CLI flag supplied by a developer running the vendoring step, not untrusted input
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

	// Clean up the upstream skeleton before layering on auth0-mock's own
	// surfaces: strip Auth0's prose, then title-case its kebab-case tag names
	// so the rendered sidebar reads consistently with our own Title Case
	// fragment tags. Both run before the fragments merge, so they only touch
	// Auth0's content.
	stripUpstreamProse(base)
	titleizeManagementTags(base)

	fragments := [][]byte{
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
// group→tag nesting is meaningful rather than redundant.
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
	admin0Tags := []string{"Claims", "Permissions", "MFA", "Expectations"}
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
	"Auth0 spec.\n" +
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

// stripUpstreamProse removes Auth0's authored prose from the loaded upstream
// spec, leaving the structural skeleton (paths, methods, parameters, schemas)
// that the mock actually needs to route and validate requests. Every
// `description` is blanked and every `externalDocs` dropped — that prose, and
// the ~100 auth0.com documentation links embedded in it, is Auth0's
// copyrightable content; the API structure itself is not. `summary` is kept:
// it's a short factual label the rendered docs sidebar depends on. `example`
// values are kept: sample data, functionally useful for
// validation and "Try it".
//
// Called before the per-surface fragments are merged, so auth0-mock's own
// authored descriptions (authapi, admin0, service) are untouched.
//
// Response.Description is blanked to "" rather than removed because OpenAPI
// requires it on every Response object — an empty string keeps the document
// structurally valid.
//
// Vendor extensions (`x-*`) are dropped wholesale: the upstream spec carries
// ~1000 `x-description-N` prose fields (plus `x-operation-name`,
// `x-release-lifecycle`, …), all of which is Auth0 content, and nothing in the
// mock's routing or validation reads any extension.
//
//nolint:gocyclo // Walks every prose-bearing field in the OpenAPI tree; splitting per-section would obscure the deliberately-exhaustive list.
func stripUpstreamProse(base *openapi3.T) {
	base.ExternalDocs = nil
	base.Extensions = nil
	// Drop Auth0's authored top-level metadata: description, contact info,
	// terms-of-service URL. The `bundle()` step rewrites `Info` to
	// auth0-mock's own identity before the merged spec is emitted, but the
	// committed skeleton ships with this struct populated, so we strip it
	// here too so the NOTICE claim ("all Auth0-authored prose removed")
	// holds for both the skeleton AND the merged spec. Title is left as the
	// upstream's literal name — it's identifying metadata, not prose
	// describing endpoints.
	if base.Info != nil {
		base.Info.Description = ""
		base.Info.TermsOfService = ""
		base.Info.Contact = nil
		base.Info.License = nil
		base.Info.Extensions = nil
	}
	// Strip the prose on each upstream Server entry without dropping the
	// list — bundle() reads Servers[0].URL to derive the /api/v2 path
	// prefix, then overwrites Servers entirely with auth0-mock's own URL.
	// We need the URL preserved here so that pipeline still works.
	for _, srv := range base.Servers {
		if srv == nil {
			continue
		}
		srv.Description = ""
		srv.Extensions = nil
		for _, v := range srv.Variables {
			if v == nil {
				continue
			}
			v.Description = ""
			v.Extensions = nil
		}
	}
	if base.Paths != nil {
		base.Paths.Extensions = nil
		for _, item := range base.Paths.Map() {
			stripPathItem(item)
		}
	}
	if base.Components == nil {
		return
	}
	base.Components.Extensions = nil
	for _, ref := range base.Components.SecuritySchemes {
		stripSecuritySchemeRef(ref)
	}
	for _, ref := range base.Components.Schemas {
		stripSchemaRef(ref)
	}
	for _, ref := range base.Components.Parameters {
		stripParameterRef(ref)
	}
	for _, ref := range base.Components.Headers {
		if ref == nil {
			continue
		}
		ref.Extensions = nil
		if ref.Value == nil {
			continue
		}
		ref.Value.Description = ""
		ref.Value.Extensions = nil
		stripSchemaRef(ref.Value.Schema)
	}
	for _, ref := range base.Components.RequestBodies {
		if ref == nil {
			continue
		}
		ref.Extensions = nil
		if ref.Value == nil {
			continue
		}
		ref.Value.Description = ""
		ref.Value.Extensions = nil
		stripContent(ref.Value.Content)
	}
	for _, ref := range base.Components.Responses {
		stripResponseRef(ref)
	}
}

func stripPathItem(item *openapi3.PathItem) {
	if item == nil {
		return
	}
	item.Description = ""
	item.Extensions = nil
	for _, p := range item.Parameters {
		stripParameterRef(p)
	}
	for _, op := range item.Operations() {
		if op == nil {
			continue
		}
		op.Description = ""
		op.ExternalDocs = nil
		op.Extensions = nil
		for _, p := range op.Parameters {
			stripParameterRef(p)
		}
		if op.RequestBody != nil && op.RequestBody.Value != nil {
			op.RequestBody.Value.Description = ""
			op.RequestBody.Value.Extensions = nil
			stripContent(op.RequestBody.Value.Content)
		}
		if op.Responses != nil {
			for _, r := range op.Responses.Map() {
				stripResponseRef(r)
			}
		}
	}
}

func stripSecuritySchemeRef(ref *openapi3.SecuritySchemeRef) {
	if ref == nil {
		return
	}
	ref.Extensions = nil
	if ref.Value == nil {
		return
	}
	ref.Value.Description = ""
	ref.Value.Extensions = nil
	if ref.Value.Flows == nil {
		return
	}
	ref.Value.Flows.Extensions = nil
	for _, flow := range []*openapi3.OAuthFlow{
		ref.Value.Flows.Implicit,
		ref.Value.Flows.Password,
		ref.Value.Flows.ClientCredentials,
		ref.Value.Flows.AuthorizationCode,
	} {
		if flow != nil {
			flow.Extensions = nil
		}
	}
}

func stripParameterRef(ref *openapi3.ParameterRef) {
	if ref == nil {
		return
	}
	ref.Extensions = nil
	if ref.Value == nil {
		return
	}
	ref.Value.Description = ""
	ref.Value.Extensions = nil
	stripSchemaRef(ref.Value.Schema)
	stripContent(ref.Value.Content)
}

func stripResponseRef(ref *openapi3.ResponseRef) {
	if ref == nil {
		return
	}
	ref.Extensions = nil
	if ref.Value == nil {
		return
	}
	// Description is required on a Response — blank it, don't drop it.
	empty := ""
	ref.Value.Description = &empty
	ref.Value.Extensions = nil
	stripContent(ref.Value.Content)
	for _, h := range ref.Value.Headers {
		if h == nil {
			continue
		}
		h.Extensions = nil
		if h.Value == nil {
			continue
		}
		h.Value.Description = ""
		h.Value.Extensions = nil
		stripSchemaRef(h.Value.Schema)
	}
}

func stripContent(content openapi3.Content) {
	for _, mt := range content {
		if mt != nil {
			mt.Extensions = nil
			stripSchemaRef(mt.Schema)
		}
	}
}

// stripSchemaRef blanks prose on an inline schema and recurses. A $ref node
// still has its *wrapper* extensions cleared (Auth0 sometimes hangs
// `x-release-lifecycle` next to a `$ref`), but the referent is left alone:
// it is a named component schema walked at its own definition site, so
// following the ref would double-process and could loop on recursive schemas.
func stripSchemaRef(ref *openapi3.SchemaRef) {
	if ref == nil {
		return
	}
	ref.Extensions = nil
	if ref.Ref != "" || ref.Value == nil {
		return
	}
	s := ref.Value
	s.Description = ""
	s.ExternalDocs = nil
	s.Extensions = nil
	if s.Discriminator != nil {
		s.Discriminator.Extensions = nil
	}
	for _, p := range s.Properties {
		stripSchemaRef(p)
	}
	for _, p := range s.PatternProperties {
		stripSchemaRef(p)
	}
	stripSchemaRef(s.Items)
	for _, sub := range s.PrefixItems {
		stripSchemaRef(sub)
	}
	for _, sub := range s.AllOf {
		stripSchemaRef(sub)
	}
	for _, sub := range s.AnyOf {
		stripSchemaRef(sub)
	}
	for _, sub := range s.OneOf {
		stripSchemaRef(sub)
	}
	stripSchemaRef(s.Not)
	stripSchemaRef(s.AdditionalProperties.Schema)
	stripSchemaRef(s.UnevaluatedProperties.Schema)
	stripSchemaRef(s.UnevaluatedItems.Schema)
}

// titleizeManagementTags rewrites the upstream Auth0 tag names on every
// operation from kebab-case ("client-grants") to Title Case ("Client Grants"),
// so the rendered docs sidebar reads consistently with auth0-mock's own
// fragment tags. Run before fragments merge, so it only touches Auth0's tags;
// run before applyTagGroups, so x-tagGroups picks up the new names.
func titleizeManagementTags(base *openapi3.T) {
	if base.Paths == nil {
		return
	}
	for _, item := range base.Paths.Map() {
		for _, op := range item.Operations() {
			if op == nil {
				continue
			}
			for i, tag := range op.Tags {
				op.Tags[i] = titleizeTag(tag)
			}
		}
	}
}

// titleizeTag turns a kebab-case tag into space-separated Title Case:
// "event-streams" -> "Event Streams". Acronyms (ACL, SCIM, …) are not
// special-cased — "Network Acls" is still a clear improvement over the raw
// "network-acls" and not worth a hand-maintained exceptions table.
func titleizeTag(tag string) string {
	parts := strings.Split(tag, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}
