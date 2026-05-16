#-----------------------------------------------------------------------------------------------------------------------
# Variables (https://www.gnu.org/software/make/manual/html_node/Using-Variables.html#Using-Variables)
#-----------------------------------------------------------------------------------------------------------------------
BINARIES_DIR = $(CURDIR)/bin
BINARY_NAME = auth0-mock

#-----------------------------------------------------------------------------------------------------------------------
# Rules (https://www.gnu.org/software/make/manual/html_node/Rule-Introduction.html#Rule-Introduction)
#-----------------------------------------------------------------------------------------------------------------------
.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help message and exit
	@awk 'BEGIN {FS = ":.*?## "; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?## / { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

$(BINARIES_DIR)/golangci-lint:
	@echo "==> Installing golangci-lint within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@ff63786c30d6c2926f99d677ab2ecf089e9390ad # v2.5.0

$(BINARIES_DIR)/commitlint:
	@echo "==> Installing commitlint within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/conventionalcommit/commitlint@e9a606ce7074ac884ea091765be1651be18356d4 # v0.10.1

$(BINARIES_DIR)/govulncheck:
	@echo "==> Installing govulncheck within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install golang.org/x/vuln/cmd/govulncheck@latest

$(BINARIES_DIR)/air:
	@echo "==> Installing air within ${BINARIES_DIR}"
	@GOBIN=$(BINARIES_DIR) go install github.com/air-verse/air@latest

.PHONY: build
build: ## Build the auth0-mock binary into bin/
	@echo "==> Building $(BINARY_NAME) within $(BINARIES_DIR)"
	@go build -v -o "$(BINARIES_DIR)/$(BINARY_NAME)" "$(CURDIR)/cmd/api/main.go"

.PHONY: test
test: ## Run unit tests with the race detector
	@go test -race -count=1 ./...

.PHONY: test-features
test-features: ## Run the godog acceptance suite
	@go test -tags=features -count=1 ./cmd/api/...

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

.PHONY: pre-commit
pre-commit: ## Install local pre-commit and commit-msg hooks
	@if ! command -v pre-commit >/dev/null 2>&1; then \
		echo "'pre-commit' is not installed. Install with 'pip install pre-commit' or 'brew install pre-commit'."; \
		exit 1; \
	fi
	@pre-commit install --hook-type pre-commit --hook-type commit-msg
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
