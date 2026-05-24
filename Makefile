#-----------------------------------------------------------------------------------------------------------------------
# Variables (https://www.gnu.org/software/make/manual/html_node/Using-Variables.html#Using-Variables)
#-----------------------------------------------------------------------------------------------------------------------
BINARIES_DIR = $(CURDIR)/bin
BINARY_NAME = auth0-mock
COVERAGE_DIR = $(CURDIR)/coverage

# Packages measured for coverage: everything except the godog acceptance-test
# harness (features/scenario), which is test scaffolding, not production code.
# Kept in sync with the `ignore` list in codecov.yml so local `make coverage`
# totals match the Codecov report.
COVERPKG = $(shell go list ./... | grep -v '/features/scenario' | paste -sd,)

# Build metadata baked into the binary via -ldflags="-X ..." so that
# `auth0-mock -version` reports something useful from local builds too
# (goreleaser overrides these with its own template values in CI).
VERSION_PKG := github.com/sergiught/auth0-mock/internal/version
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
               -X $(VERSION_PKG).Version=$(VERSION) \
               -X $(VERSION_PKG).Commit=$(COMMIT) \
               -X $(VERSION_PKG).Date=$(BUILD_DATE)

#-----------------------------------------------------------------------------------------------------------------------
# Help (default goal — `make` with no args prints the target catalogue)
#-----------------------------------------------------------------------------------------------------------------------
.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help message and exit
	@awk 'BEGIN {FS = ":.*?## "; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?## / { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

#-----------------------------------------------------------------------------------------------------------------------
# Tooling (SHA-pinned helpers installed into ./bin on first use)
#-----------------------------------------------------------------------------------------------------------------------
$(BINARIES_DIR)/golangci-lint:
	@echo "==> Installing golangci-lint within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@c0d3ddc9cf3faa61a4e378e879ece580256d76e5 # v2.12.2

$(BINARIES_DIR)/commitlint:
	@echo "==> Installing commitlint within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/conventionalcommit/commitlint@e9a606ce7074ac884ea091765be1651be18356d4 # v0.10.1

$(BINARIES_DIR)/govulncheck:
	@echo "==> Installing govulncheck within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install golang.org/x/vuln/cmd/govulncheck@0782b76014f15f24e22a438f30f308df42899ba1 # v1.3.0

$(BINARIES_DIR)/air:
	@echo "==> Installing air within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/air-verse/air@3df4a176ee4896be4a4485a6a2dd85f7583534dc # v1.65.1

$(BINARIES_DIR)/go-licenses:
	@echo "==> Installing go-licenses within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/google/go-licenses@5348b744d0983d85713295ea08a20cca1654a45e # v1.6.0

#-----------------------------------------------------------------------------------------------------------------------
# Build (https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: build
build: ## Build the auth0-mock binary into bin/
	@echo "==> Building $(BINARY_NAME) $(VERSION) within $(BINARIES_DIR)"
	@go build -v -ldflags="$(LDFLAGS)" -o "$(BINARIES_DIR)/$(BINARY_NAME)" "$(CURDIR)/cmd/api/main.go"

.PHONY: build-with-cover
build-with-cover: ## Build a coverage-instrumented binary that emits covdata to GOCOVERDIR at runtime
	@echo "==> Building $(BINARY_NAME) $(VERSION) (coverage-instrumented) within $(BINARIES_DIR)"
	@go build -cover -coverpkg=./... -ldflags="$(LDFLAGS)" -o "$(BINARIES_DIR)/$(BINARY_NAME)" "$(CURDIR)/cmd/api/main.go"

#-----------------------------------------------------------------------------------------------------------------------
# Test (https://pkg.go.dev/testing — unit + godog acceptance + coverage)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: test
test: ## Run unit tests with the race detector
	@go test -race -count=1 ./...

.PHONY: test-features
test-features: ## Run the godog acceptance suite
	@go test -tags=features -count=1 ./cmd/api/...

.PHONY: test-cover
test-cover: ## Run unit tests with coverage -> coverage/unit.out
	@mkdir -p $(COVERAGE_DIR)
	@echo "==> Running unit tests with coverage"
	@go test -race -count=1 -coverpkg=$(COVERPKG) -coverprofile=$(COVERAGE_DIR)/unit.out ./...
	@go tool cover -func=$(COVERAGE_DIR)/unit.out | tail -1

.PHONY: test-features-cover
test-features-cover: ## Run the godog suite with coverage -> coverage/features.out
	@mkdir -p $(COVERAGE_DIR)
	@echo "==> Running godog acceptance suite with coverage"
	@go test -tags=features -count=1 -coverpkg=$(COVERPKG) -coverprofile=$(COVERAGE_DIR)/features.out ./cmd/api/...
	@go tool cover -func=$(COVERAGE_DIR)/features.out | tail -1

.PHONY: coverage
coverage: test-cover test-features-cover ## Run all tests with coverage and print per-suite totals
	@echo "==> Coverage files written under $(COVERAGE_DIR)/"
	@printf "  unit:     %s\n" "$$(go tool cover -func=$(COVERAGE_DIR)/unit.out | tail -1 | awk '{print $$3}')"
	@printf "  features: %s\n" "$$(go tool cover -func=$(COVERAGE_DIR)/features.out | tail -1 | awk '{print $$3}')"

#-----------------------------------------------------------------------------------------------------------------------
# Lint & security (golangci-lint, commitlint, govulncheck)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: lint
lint: $(BINARIES_DIR)/golangci-lint ## Run golangci-lint over the project (with --fix)
	@echo "==> Running golangci-lint"
	@$(BINARIES_DIR)/golangci-lint run -v --fix -c .golangci.yaml ./...

.PHONY: lint-commits
lint-commits: $(BINARIES_DIR)/commitlint ## Lint the current commit message against commitlint.yaml
	@$(BINARIES_DIR)/commitlint lint

.PHONY: vuln
vuln: $(BINARIES_DIR)/govulncheck ## Scan the module graph for known Go vulnerabilities
	@echo "==> Scanning module graph for known Go vulnerabilities"
	@$(BINARIES_DIR)/govulncheck ./...

.PHONY: licenses
licenses: $(BINARIES_DIR)/go-licenses ## Save bundled-module license texts under licenses/ for release archives
	@echo "==> Collecting third-party license texts -> licenses/"
	@rm -rf licenses
	@$(BINARIES_DIR)/go-licenses save ./cmd/api --save_path=licenses --force
	@echo "==> Wrote $$(find licenses -name LICENSE -o -name LICENCE -o -name 'LICEN[SC]E.*' | wc -l) LICENSE files under licenses/"

#-----------------------------------------------------------------------------------------------------------------------
# OpenAPI spec (https://www.openapis.org — merge fragments + re-vendor skeleton)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: openapi
openapi: ## Regenerate the merged OpenAPI spec at api/auth0-mock.openapi.json
	@echo "==> Generating merged OpenAPI spec"
	@go run ./cmd/genopenapi -out api/auth0-mock.openapi.json

.PHONY: refresh-spec
refresh-spec: ## Re-vendor the Auth0 Management API skeleton from a manually-downloaded raw spec
	@test -f api/auth0-management-api.raw.json || { \
		echo "Place the manually-downloaded Auth0 Management API OpenAPI spec at"; \
		echo "  api/auth0-management-api.raw.json"; \
		echo "(it is gitignored — only the stripped skeleton is committed)"; \
		exit 1; }
	@echo "==> Stripping Auth0 prose -> api/auth0-management-api.openapi.json"
	@go run ./cmd/genopenapi -strip-raw api/auth0-management-api.raw.json -out api/auth0-management-api.openapi.json
	@$(MAKE) openapi

#-----------------------------------------------------------------------------------------------------------------------
# Local development (env scaffolding + hot reload + docker compose)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: pre-commit
pre-commit: ## Install local pre-commit and commit-msg hooks
	@if ! command -v pre-commit >/dev/null 2>&1; then \
		echo "'pre-commit' is not installed. Install with one of:"; \
		echo "  pipx install pre-commit       # recommended on PEP-668 distros (Arch, Debian 12+)"; \
		echo "  brew install pre-commit       # macOS"; \
		echo "  pip install --user pre-commit # any other Python environment"; \
		exit 1; \
	fi
	@pre-commit install --hook-type pre-commit --hook-type commit-msg --hook-type pre-push
	@echo "==> pre-commit hooks installed"

.PHONY: dev-env
dev-env: ## Materialise .env from .env.example (no-op if .env already exists)
	@cp -n .env.example .env || true

.PHONY: watch
watch: dev-env $(BINARIES_DIR)/air ## Run the API locally with native hot reload via air
	@# Source .env before launching air — neither air nor the binary
	@# auto-loads it, and dev flow keys (DEBUG=true, SIGNING_KEY_FILE
	@# to survive hot reload) live there. `set -a` exports every
	@# subsequently-assigned var; `set +a` switches it off again.
	@set -a; [ -f .env ] && . ./.env; set +a; $(BINARIES_DIR)/air

.PHONY: dev-run
dev-run: dev-env ## Run the API inside docker compose and tail its logs
	@docker compose up -d --build
	@docker compose logs -f auth0-mock

#-----------------------------------------------------------------------------------------------------------------------
# Release (https://goreleaser.com — local dry-run of the full release pipeline)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: release-dry-run
release-dry-run: ## Build a full release locally without publishing — exercises goreleaser, multi-arch Docker, SBOMs, and Cosign signing in --skip mode
	@command -v goreleaser >/dev/null 2>&1 || { \
		echo "goreleaser not installed. Install with:"; \
		echo "  go install github.com/goreleaser/goreleaser/v2@latest"; \
		exit 1; }
	@command -v syft >/dev/null 2>&1 || { \
		echo "syft not installed (needed for SBOM generation). Install with:"; \
		echo "  brew install syft  # or see https://github.com/anchore/syft"; \
		exit 1; }
	@echo "==> Running goreleaser in dry-run mode"
	@goreleaser release --snapshot --clean --skip=publish,sign

#-----------------------------------------------------------------------------------------------------------------------
# Demo (drives the go-auth0 SDK example against a throwaway mock instance)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: demo
demo: build ## Run the go-auth0 SDK example against a throwaway mock instance
	@# Port-busy precheck: without it, if anything (a stale auth0-mock,
	@# a docker compose run, an unrelated service on :8443) already
	@# holds the port, the demo silently connects to that *other* server
	@# and surfaces a baffling TLS cert mismatch instead of the actual
	@# "port in use" error. Three-tier detection:
	@#   1) ss   — Linux, visible to non-root even when the listener is
	@#             in a container.
	@#   2) lsof — macOS / BSD, plus naming on Linux (command[pid] when
	@#             lsof can see the process owner).
	@#   3) TCP  — last-resort connect probe so macOS dev boxes with
	@#             root-owned listeners (where lsof can't attribute the
	@#             process) still catch the port collision.
	@busy=""; busy_who=""; \
	if command -v ss >/dev/null 2>&1; then \
		ss -tlnH 'sport = :8443' 2>/dev/null | grep -q . && busy="yes"; \
	fi; \
	if command -v lsof >/dev/null 2>&1; then \
		busy_who=$$(lsof -nP -i :8443 -sTCP:LISTEN 2>/dev/null | awk 'NR>1 {print $$1"["$$2"]"; exit}'); \
		[ -n "$$busy_who" ] && busy="yes"; \
	fi; \
	if [ -z "$$busy" ] && command -v nc >/dev/null 2>&1; then \
		nc -z -w 1 127.0.0.1 8443 >/dev/null 2>&1 && busy="yes"; \
	fi; \
	if [ "$$busy" = "yes" ]; then \
		if [ -n "$$busy_who" ]; then \
			echo "==> ERROR: localhost:8443 is already in use by $$busy_who. Stop it and re-run \`make demo\`."; \
		else \
			echo "==> ERROR: localhost:8443 is already in use. Run \`sudo lsof -i :8443\` to identify the listener, stop it, and re-run \`make demo\`."; \
		fi; \
		exit 1; \
	fi
	@echo "==> Starting $(BINARY_NAME) for the demo (with persisted TLS cert)"
	@TLS_DIR=/tmp/auth0-mock-demo-tls; \
	mkdir -p $$TLS_DIR; \
	TLS_CACHE_DIR=$$TLS_DIR "$(BINARIES_DIR)/$(BINARY_NAME)" > /tmp/auth0-mock-demo.log 2>&1 & \
	MOCK_PID=$$!; \
	trap 'kill $$MOCK_PID 2>/dev/null' EXIT; \
	for i in $$(seq 1 50); do \
		curl -sk https://localhost:8443/healthz >/dev/null 2>&1 && [ -f $$TLS_DIR/tls.crt ] && break; \
		sleep 0.2; \
	done; \
	echo "==> Running examples/consumer against the mock"; \
	cd examples/consumer && go run . -cert=$$TLS_DIR/tls.crt

#-----------------------------------------------------------------------------------------------------------------------
# Demo-sdk (drives the pkg/auth0mock Go SDK + the real go-auth0 SDK against a throwaway mock)
#-----------------------------------------------------------------------------------------------------------------------
.PHONY: demo-sdk
demo-sdk: build ## Run the pkg/auth0mock SDK + go-auth0 round-trip against a throwaway mock
	@# The SDK example now drives stubs through the real go-auth0 SDK,
	@# which only speaks HTTPS — so the mock has to boot on :8443 with
	@# a persisted TLS cert that the example can trust (same setup
	@# `make demo` uses for the consumer example).
	@if command -v nc >/dev/null 2>&1 && nc -z -w 1 127.0.0.1 8443 >/dev/null 2>&1; then \
		echo "==> ERROR: localhost:8443 is already in use. Stop the listener and re-run \`make demo-sdk\`."; \
		exit 1; \
	fi
	@# Refresh examples/sdk's vendor tree. The example uses a local-path
	@# `replace` directive to pick up in-tree changes to pkg/auth0mock,
	@# but Go uses the vendor copy when one exists — and examples/sdk
	@# vendors its deps + ignores the vendor dir from git. On a fresh
	@# clone (or after the SDK gains new symbols), the vendor tree is
	@# stale and `go run .` fails with "undefined" errors. Refresh up
	@# front so the demo always reflects the current SDK.
	@echo "==> Refreshing examples/sdk vendor against in-tree pkg/auth0mock"
	@cd examples/sdk && go mod vendor
	@echo "==> Starting $(BINARY_NAME) for the SDK demo (with persisted TLS cert)"
	@TLS_DIR=/tmp/auth0-mock-demo-sdk-tls; \
	mkdir -p $$TLS_DIR; \
	TLS_CACHE_DIR=$$TLS_DIR "$(BINARIES_DIR)/$(BINARY_NAME)" > /tmp/auth0-mock-demo-sdk.log 2>&1 & \
	MOCK_PID=$$!; \
	trap 'kill $$MOCK_PID 2>/dev/null' EXIT; \
	for i in $$(seq 1 50); do \
		curl -sk https://localhost:8443/healthz >/dev/null 2>&1 && [ -f $$TLS_DIR/tls.crt ] && break; \
		sleep 0.2; \
	done; \
	echo "==> Running examples/sdk against the mock"; \
	cd examples/sdk && go run . -cert=$$TLS_DIR/tls.crt
