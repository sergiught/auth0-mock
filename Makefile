BINARIES_DIR = $(CURDIR)/bin
BINARY_NAME = auth0-mock

# Tooling versions (pinned to commit SHAs for reproducibility).
GOLANGCI_LINT_VERSION = v2.5.0
GOLANGCI_LINT_SHA     = ff63786c30d6c2926f99d677ab2ecf089e9390ad
COMMITLINT_VERSION    = v0.10.1
COMMITLINT_SHA        = e9a606ce7074ac884ea091765be1651be18356d4
GOVULNCHECK_REF       = latest
AIR_REF               = latest

.PHONY: build
build:
	@echo "==> Building $(BINARY_NAME) into $(BINARIES_DIR)"
	@go build -v -o "$(BINARIES_DIR)/$(BINARY_NAME)" "$(CURDIR)/cmd/api/main.go"

.PHONY: test
test:
	@go test -race -count=1 ./...

.PHONY: test-features
test-features:
	@go test -tags=features -count=1 ./cmd/api/...

# ---- Tooling installs (idempotent; pinned where possible) ----

$(BINARIES_DIR)/golangci-lint:
	@echo "==> Installing golangci-lint $(GOLANGCI_LINT_VERSION) into $(BINARIES_DIR)"
	@GOBIN=$(BINARIES_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_SHA)

$(BINARIES_DIR)/commitlint:
	@echo "==> Installing commitlint $(COMMITLINT_VERSION) into $(BINARIES_DIR)"
	@GOBIN=$(BINARIES_DIR) go install github.com/conventionalcommit/commitlint@$(COMMITLINT_SHA)

$(BINARIES_DIR)/govulncheck:
	@echo "==> Installing govulncheck@$(GOVULNCHECK_REF) into $(BINARIES_DIR)"
	@GOBIN=$(BINARIES_DIR) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_REF)

$(BINARIES_DIR)/air:
	@echo "==> Installing air into $(BINARIES_DIR)"
	@GOBIN=$(BINARIES_DIR) go install github.com/air-verse/air@$(AIR_REF)

# ---- Quality gates ----

.PHONY: lint
lint: $(BINARIES_DIR)/golangci-lint
	@echo "==> Running golangci-lint"
	@$(BINARIES_DIR)/golangci-lint run -v --fix -c .golangci.yaml ./...

.PHONY: lint-commits
lint-commits: $(BINARIES_DIR)/commitlint
	@$(BINARIES_DIR)/commitlint lint

.PHONY: vuln
vuln: $(BINARIES_DIR)/govulncheck
	@echo "==> Scanning module graph for known Go vulnerabilities"
	@$(BINARIES_DIR)/govulncheck ./...

# ---- Pre-commit hooks ----

.PHONY: pre-commit
pre-commit:
	@if ! command -v pre-commit >/dev/null 2>&1; then \
		echo "⚠️  'pre-commit' is not installed. Install with 'pip install pre-commit' or 'brew install pre-commit'."; \
		exit 1; \
	fi
	@pre-commit install --hook-type pre-commit --hook-type commit-msg
	@echo "==> pre-commit hooks installed"

# ---- Local dev loop ----

.PHONY: dev-env
dev-env:
	@cp -n .env.example .env || true

.PHONY: watch
watch: dev-env $(BINARIES_DIR)/air
	@$(BINARIES_DIR)/air

.PHONY: dev-run
dev-run: dev-env
	@docker compose up -d --build
	@docker compose logs -f auth0-mock
