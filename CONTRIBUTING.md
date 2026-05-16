# Contributing

Thanks for considering a contribution. This document covers everything you need to get a change merged: local setup, code conventions, testing, and how to add new functionality to each part of the mock.

## 📐 Project layout

```
auth0-mock/
├── cmd/api/                        # main entrypoint (binary)
├── api/                            # embedded Auth0 API skeleton + merged spec (//go:embed)
├── internal/
│   ├── config/                     # envconfig settings
│   ├── logger/                     # zerolog setup
│   ├── server/                     # HTTP/HTTPS server lifecycle + orchestrator
│   ├── tlscert/                    # self-signed cert generator + loader
│   ├── matches/                    # in-memory Management API stub store
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
│   ├── mgmtapi/                    # spec-driven Management API handlers
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

Day-to-day iteration uses **`make watch`**, it installs [`air`](https://github.com/air-verse/air) into `./bin` on first run, then watches `cmd/` + `internal/` + `api/` and rebuilds + restarts the binary on every save. Sub-second loop; no docker.

The Auth0 Management API skeleton is already committed at `api/auth0-management-api.openapi.json` — nothing to download to build or run. (Refreshing it from a newer Auth0 spec is a separate, deliberate step — see [Refreshing the Auth0 Management API spec](#refreshing-the-auth0-management-api-spec).)

## 🧪 Testing

```bash
make test                  # unit tests with the race detector
make test-features         # godog acceptance suite (in-process, random port)
```

**Both must be green for a PR to merge.** The godog suite boots the service in-process on a random port and exercises every endpoint end-to-end with a broad library of acceptance scenarios under [`features/`](features/).

### Coverage

Coverage profiles land in `coverage/` (gitignored). Both suites use `-coverpkg=./...` so per-package numbers reflect end-to-end coverage from each path.

```bash
make test-cover            # unit suite → coverage/unit.out
make test-features-cover   # godog suite → coverage/features.out
make coverage              # both, plus per-suite percentage summary
```

CI uploads each profile to Codecov with separate `unit` and `features` flags; Codecov merges them server-side, so the public coverage figure reflects both paths combined.

### What needs which kind of test

| Change kind | Required tests |
|---|---|
| New `internal/*` package | Unit tests per package (table-driven; `-race`-safe) |
| New Auth API endpoint | Unit test in `internal/authapi/*_test.go` **and** a godog scenario |
| New admin0 endpoint | Unit test in `internal/admin0/admin0_test.go` **and** a godog scenario |
| New OAuth grant | Godog scenario in `features/OAuthGrants.feature` (or a new file) |
| Management API behaviour change | Godog scenario in `features/{Expectations,PatternFallback,Reset,BearerEnforcement}.feature` |
| Bug fix | A failing test first, then the fix |

### Adding a godog scenario

1. Pick or add a `.feature` file under `features/`.
2. Use existing step phrases when possible, see [`features/scenario/steps.go`](features/scenario/steps.go) for the catalogue. Common phrases:
   - `Given the mock is running`
   - `And I have a valid bearer token`
   - `When I PUT "/admin0/..." with body:`
   - `When I post to "/oauth/token" with form body:`
   - `Then I receive a 200 response`
   - `And the response JSON path "x.y" equals "z"`
   - `And the access_token claim "permissions" array contains "read:users"`
3. Run `make test-features` and iterate.

If you need a new step phrase, add it to `features/scenario/steps.go` and write a focused unit test for the helper if the logic is non-trivial.

## 🔍 Lint, security, commit checks

The project ships a `.golangci.yaml`, `commitlint.yaml`, and `.pre-commit-config.yaml`. They are wired into both the Makefile and GitHub Actions CI, keep them green locally before pushing.

```bash
make lint            # golangci-lint v2.5.0 against ./...
make lint-commits    # commitlint against your last N commits
make vuln            # govulncheck against the module graph
make pre-commit      # install the pre-commit hooks (one-time)
```

`make pre-commit` runs the [pre-commit](https://pre-commit.com) framework, install it once and the hooks will run `gofmt`, `golangci-lint` on every `git commit`, `govulncheck` on every `git push`, plus `commitlint` on the commit message. CI runs the same checks on every PR (see `.github/workflows/ci.yml`).

> **Prerequisite:** `make pre-commit` needs the `pre-commit` CLI on `PATH` first. Install with `pipx install pre-commit` (recommended), `brew install pre-commit`, or `pip install --user pre-commit`. The target's first line checks for it and tells you which install command to run if missing.

GitHub Actions also runs **CodeQL** (`.github/workflows/codeql.yml`) on every push, every PR, and a weekly Monday cron, using the `security-and-quality` query suite. Findings appear in the repo's **Security tab → Code scanning alerts**. If your PR introduces a CodeQL alert, fix it before merging or open a discussion if it's a false positive — the suite is broader than the default and occasionally lights up benign code.

## ✍️ Code style

Standard Go formatting plus a few specific conventions:

- `gofmt -l .` must print nothing. PRs that fail this are CI-rejected.
- `go vet ./...` must be clean.
- `make lint` must report `0 issues`.
- Every handler is a **struct holding its dependencies as fields**, implementing `ServeHTTP(w, r)`. Closures-returning-handlers are a code smell here.
- Mount functions take a `chi.Router` and register handlers via `r.Method(verb, path, &Handler{Deps: ...})`.
- JSON responses use `render.JSON(w, r, body)`. Use `render.Status(r, code)` before `render.JSON` for non-200.
- Error responses use `httperr.WriteMgmt(...)` for `/api/v2/*` and `/admin0/*`, `httperr.WriteAuth(...)` for `/oauth/*`, `/authorize`, `/dbconnections/*`, `/passwordless/*`.
- Each new package gets a one-paragraph doc comment on its `package` line explaining its purpose.
- New env vars are added to `internal/config/config.go` AND `.env.example` AND the README's Configuration table.

### Commit messages

Conventional commits, same shape as the existing history:

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

You probably don't need to do anything. The Management API is **spec-driven**: every operation in the embedded `api/auth0-management-api.openapi.json` gets one bearer-protected generic handler registered automatically by `mgmtapi.Mount`. Canned responses are registered out-of-band via `POST /admin0/expectations` (see the admin0 section above).

### Refreshing the Auth0 Management API spec

`api/auth0-management-api.openapi.json` is **not** Auth0's published spec verbatim — it is a stripped *skeleton*: paths, methods, parameters, and schema shapes only. Every Auth0-authored `description`, `externalDocs` link, and `x-*` extension has been removed (see `stripUpstreamProse` in `cmd/genopenapi/main.go`). The skeleton is what the mock needs to route and validate; Auth0's prose is not, and is not ours to redistribute. The raw download is gitignored (`/api/*.raw.json`) and never committed.

To pull in a newer Auth0 spec:

1. **Manually** download the current Auth0 Management API OpenAPI document and save it to `api/auth0-management-api.raw.json` (a deliberate, one-time human action — nothing in this repo scrapes Auth0).
2. Run `make refresh-spec`. It strips the raw file into the committed skeleton and regenerates the merged `api/auth0-mock.openapi.json`.
3. Run `make test`, review the skeleton diff, and open a PR. The repo will drift from upstream between refreshes — that's expected; the mock's behaviour is pinned by tests, not by being current.

If you need to **change the generic handler's behaviour** (e.g. inject latency, log differently), edit `internal/mgmtapi/handler.go`, but keep in mind it serves all ~400 endpoints uniformly.

### A new test recipe

If your work surfaces a recipe other users will want (e.g. "how do I test refresh-token rotation"), add a section to [`docs/COOKBOOK.md`](docs/COOKBOOK.md).

## 🚦 PR workflow

1. Open an issue first for anything non-trivial, saves rework if the maintainers think differently about the design.
2. Branch from `main`. One coherent change per PR; smaller is better.
3. **Don't hand-edit `CHANGELOG.md`** — release-please owns it. Just write a conventional-commit subject and body that explains the *why*; the next Release PR derives entries from those automatically.
4. If you add a new env var, endpoint, or grant: update **README** and **`docs/ARCHITECTURE.md`** alongside the code.
5. CI must pass: `make lint`, `make test`, `make test-features`, `make vuln`, and `make lint-commits` on PR commits.
6. Squash-merge is the default. Keep the commit subject conventional-commit shaped.

## 🙅 What we won't accept

Some asks are out of scope:

- **Stateful Management API CRUD**: auth0-mock is a stub registrar, not a state machine. Tests register the response they want; the mock doesn't track "the user with id X was created and now exists". If you need stateful behaviour, layer it in your test fixtures.
- **Production-grade OIDC certification**: we're a mock. Spec compliance is best-effort; we deliberately skip things like full client-secret-jwt validation, full PKCE plain-method rejection toggles, etc.
- **Other IdPs** (Okta, Cognito, etc.): the project is Auth0-shaped end-to-end. A separate project is the right answer.
- **Persistence to disk for registered stubs**: explicit non-goal. Each restart is a clean slate; that's a feature.

## 📬 Questions?

Open an issue. Tag it `question` if you're not sure whether a change makes sense.
