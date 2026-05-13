# Contributing

Thanks for considering a contribution. This document covers everything you need to get a change merged: local setup, code conventions, testing, and how to add new functionality to each part of the mock.

## 📐 Project layout

```
auth0-mock/
├── cmd/api/                        # main entrypoint (binary)
├── api/                            # embedded Auth0 OpenAPI spec (//go:embed)
├── internal/
│   ├── config/                     # envconfig settings
│   ├── logger/                     # zerolog setup
│   ├── server/                     # HTTP/HTTPS server lifecycle + orchestrator
│   ├── tlscert/                    # self-signed cert generator + loader
│   ├── matches/                    # in-memory Mgmt API stub store
│   ├── claims/                     # in-memory custom-claim map
│   ├── permissions/                # in-memory per-audience RBAC store
│   ├── pkce/                       # PKCE code/challenge store
│   ├── mfa/                        # mfa_token store + global "required" flag
│   ├── spec/                       # kin-openapi loader + validators
│   ├── jwks/                       # RS256 key + JWT mint/verify + JWKS JSON
│   ├── bearer/                     # bearer-token middleware
│   ├── middleware/                 # logging, recovery, request_id
│   ├── httperr/                    # JSON error writer (Mgmt + Auth shapes)
│   ├── authapi/                    # hand-coded Auth API handlers
│   ├── mgmtapi/                    # spec-driven Mgmt API handlers
│   ├── admin0/                     # /admin0/* handlers
│   └── router/                     # composes everything into http.Handler
├── features/                       # godog acceptance tests
├── examples/consumer/              # end-to-end Go consumer example
└── docs/                           # public docs (ARCHITECTURE, COOKBOOK, COMPARISON)
```

`internal/` is private to the module. Every handler is a struct holding its dependencies as fields and implementing `ServeHTTP`. JSON responses go through `go-chi/render`. Errors go through `internal/httperr` (Mgmt-shape vs Auth-shape).

For the canonical deep-dive, see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## 🔧 Local setup

You need Go 1.26+. Everything else (kin-openapi, chi, godog, etc.) comes via `go mod`.

```bash
git clone https://github.com/sergiught/auth0-mock.git
cd auth0-mock
make build                   # builds ./bin/auth0-mock
./bin/auth0-mock             # runs on :8080 + :8443
```

The Auth0 OpenAPI spec is already embedded at `api/auth0-management-api.openapi.json` — nothing to download.

## 🧪 Testing

```bash
make test                            # = go test -race -count=1 ./...
go test -tags=features ./cmd/api/... # godog acceptance suite
```

**Both must be green for a PR to merge.** The godog suite boots the service in-process on a random port and exercises every endpoint end-to-end (63 scenarios across 16 feature files at last count).

### What needs which kind of test

| Change kind | Required tests |
|---|---|
| New `internal/*` package | Unit tests per package (table-driven; `-race`-safe) |
| New Auth API endpoint | Unit test in `internal/authapi/*_test.go` **and** a godog scenario |
| New admin0 endpoint | Unit test in `internal/admin0/admin0_test.go` **and** a godog scenario |
| New OAuth grant | Godog scenario in `features/OAuthGrants.feature` (or a new file) |
| Mgmt API behaviour change | Godog scenario in `features/{MatchRegistration,PatternFallback,Reset,BearerEnforcement}.feature` |
| Bug fix | A failing test first, then the fix |

### Adding a godog scenario

1. Pick or add a `.feature` file under `features/`.
2. Use existing step phrases when possible — see [`features/scenario/steps.go`](features/scenario/steps.go) for the catalogue. Common phrases:
   - `Given the mock is running`
   - `And I have a valid bearer token`
   - `When I PUT "/admin0/..." with body:`
   - `When I post to "/oauth/token" with form body:`
   - `Then I receive a 200 response`
   - `And the response JSON path "x.y" equals "z"`
   - `And the access_token claim "permissions" array contains "read:users"`
3. Run `go test -tags=features ./cmd/api/...` and iterate.

If you need a new step phrase, add it to `features/scenario/steps.go` and write a focused unit test for the helper if the logic is non-trivial.

## ✍️ Code style

Standard Go formatting plus a few specific conventions:

- `gofmt -l .` must print nothing. PRs that fail this are CI-rejected.
- `go vet ./...` must be clean.
- Every handler is a **struct holding its dependencies as fields**, implementing `ServeHTTP(w, r)`. Closures-returning-handlers are a code smell here.
- Mount functions take a `chi.Router` and register handlers via `r.Method(verb, path, &Handler{Deps: ...})`.
- JSON responses use `render.JSON(w, r, body)`. Use `render.Status(r, code)` before `render.JSON` for non-200.
- Error responses use `httperr.WriteMgmt(...)` for `/api/v2/*` and `/admin0/*`, `httperr.WriteAuth(...)` for `/oauth/*`, `/authorize`, `/dbconnections/*`, `/passwordless/*`.
- Each new package gets a one-paragraph doc comment on its `package` line explaining its purpose.
- New env vars are added to `internal/config/config.go` AND `.env.example` AND the README's Configuration table.

### Commit messages

Conventional commits — same shape as the existing history:

```
feat(authapi): add /oauth/introspect (RFC 7662)
fix(mgmtapi): handle empty Authorization header
test(features): scenario for empty audiences
docs(readme): add macOS trust-store recipe
chore: bump go-chi/chi to v5.2.6
refactor(jwks): extract algorithm dispatch
```

One subject line ≤ 72 chars, blank line, body wrapping at ~80 chars explaining **why** the change matters. Reference issues/PRs in the body if applicable.

## 🛣 How to add things

### A new OAuth grant

1. Add a new switch arm in `internal/authapi/token.go`'s `ServeHTTP`:
   ```go
   case "http://auth0.com/oauth/grant-type/your-grant":
       h.respondYourGrant(w, r, req, aud)
   ```
2. Add a `respondYourGrant` method on `*TokenHandler` that builds `jwks.MintOpts`, calls `h.augmentExtra(extra, audience)` to layer permissions + custom claims, and writes the response with `render.JSON`.
3. Extend `tokenRequest` in `internal/authapi/types.go` if the grant needs new form fields, AND add them to the form-decode block in `parseTokenRequest`.
4. Godog scenarios in `features/OAuthGrants.feature` (or a new feature file).

### A new admin0 endpoint

1. Add a new handler struct + `ServeHTTP` in `internal/admin0/admin0.go`.
2. Wire it in `Mount`:
   ```go
   r.Method(http.MethodGet, "/admin0/your-thing", &YourHandler{Deps: d.Whatever})
   ```
3. If it needs a new in-process store, add the package under `internal/` first, plumb it through `admin0.Deps`, `router.Deps`, `cmd/api/main.go`, and `features/scenario/context.go`. The four are the canonical wiring path.
4. Unit test in `internal/admin0/admin0_test.go`. Godog scenario in `features/`.

### A new Auth0 Management API endpoint

You probably don't need to do anything. The Mgmt API is **spec-driven** — every operation in the embedded `api/auth0-management-api.openapi.json` already has its three routes (`<verb> <path>`, `<verb> <path>/match`, `<verb> <path>/reset`) registered automatically by `mgmtapi.Mount`.

If Auth0 publishes a new endpoint:

1. Re-download the spec to `api/auth0-management-api.openapi.json`.
2. Run `go test ./...`. Boot the service. The new endpoint is live with no code changes.

If you need to **change the generic handler's behaviour** (e.g. inject latency, log differently), edit `internal/mgmtapi/handler.go` — but keep in mind it serves all ~400 endpoints uniformly.

### A new test recipe

If your work surfaces a recipe other users will want (e.g. "how do I test refresh-token rotation"), add a section to [`docs/COOKBOOK.md`](docs/COOKBOOK.md).

## 🚦 PR workflow

1. Open an issue first for anything non-trivial — saves rework if the maintainers think differently about the design.
2. Branch from `main`. One coherent change per PR; smaller is better.
3. Update `CHANGELOG.md` under `## [Unreleased]` with a one-line entry under the appropriate section (Added / Changed / Fixed / Removed).
4. If you add a new env var, endpoint, or grant: update **README** and **`docs/ARCHITECTURE.md`** alongside the code.
5. CI must pass: `make test`, `gofmt -l .`, `go vet ./...`, godog suite.
6. Squash-merge is the default. Keep the commit subject conventional-commit shaped.

## 🙅 What we won't accept

Some asks are out of scope:

- **Stateful Mgmt API CRUD** — auth0-mock is a stub registrar, not a state machine. Tests register the response they want; the mock doesn't track "the user with id X was created and now exists". If you need a stateful mock, [Keycloak](https://www.keycloak.org/) exists.
- **Production-grade OIDC certification** — we're a mock. Spec compliance is best-effort; we deliberately skip things like full client-secret-jwt validation, full PKCE plain-method rejection toggles, etc.
- **Other IdPs** (Okta, Cognito, etc.) — the project is Auth0-shaped end-to-end. A separate project is the right answer.
- **Persistence to disk for registered stubs** — explicit non-goal. Each restart is a clean slate; that's a feature.

## 📬 Questions?

Open an issue. Tag it `question` if you're not sure whether a change makes sense.
