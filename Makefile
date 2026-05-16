#-----------------------------------------------------------------------------------------------------------------------
# Variables (https://www.gnu.org/software/make/manual/html_node/Using-Variables.html#Using-Variables)
#-----------------------------------------------------------------------------------------------------------------------
BINARIES_DIR = $(CURDIR)/bin
BINARY_NAME = auth0-mock
COVERAGE_DIR = $(CURDIR)/coverage

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
	@GOBIN=$(BINARIES_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@ff63786c30d6c2926f99d677ab2ecf089e9390ad # v2.5.0

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
	@go test -race -count=1 -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/unit.out ./...
	@go tool cover -func=$(COVERAGE_DIR)/unit.out | tail -1

.PHONY: test-features-cover
test-features-cover: ## Run the godog suite with coverage -> coverage/features.out
	@mkdir -p $(COVERAGE_DIR)
	@echo "==> Running godog acceptance suite with coverage"
	@go test -tags=features -count=1 -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/features.out ./cmd/api/...
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
licenses: $(BINARIES_DIR)/go-licenses ## Save bundled-module license texts under dist/licenses/ for release archives
	@echo "==> Collecting third-party license texts -> dist/licenses/"
	@rm -rf dist/licenses
	@$(BINARIES_DIR)/go-licenses save ./cmd/api --save_path=dist/licenses --force
	@$(BINARIES_DIR)/go-licenses report ./cmd/api --template=hack/licenses.tmpl > dist/licenses/THIRD_PARTY_LICENSES.md
	@echo "==> Wrote dist/licenses/THIRD_PARTY_LICENSES.md ($$(wc -l < dist/licenses/THIRD_PARTY_LICENSES.md) lines)"

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
		echo "'pre-commit' is not installed. Install with 'pip install pre-commit' or 'brew install pre-commit'."; \
		exit 1; \
	fi
	@pre-commit install --hook-type pre-commit --hook-type commit-msg --hook-type pre-push
	@echo "==> pre-commit hooks installed"

.PHONY: dev-env
dev-env: ## Materialise .env from .env.example (no-op if .env already exists)
	@cp -n .env.example .env || true

.PHONY: watch
watch: dev-env $(BINARIES_DIR)/air ## Run the API locally with native hot reload via air
	@$(BINARIES_DIR)/air

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
