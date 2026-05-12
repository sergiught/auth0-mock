BINARIES_DIR = $(CURDIR)/bin
BINARY_NAME = auth0-mock

.PHONY: build
build:
	@echo "==> Building $(BINARY_NAME) into $(BINARIES_DIR)"
	@go build -v -o "$(BINARIES_DIR)/$(BINARY_NAME)" "$(CURDIR)/cmd/api/main.go"

.PHONY: test
test:
	@go test -race -count=1 ./...

.PHONY: lint
lint:
	@go vet ./...

.PHONY: dev-env
dev-env:
	@cp -n .env.example .env || true

.PHONY: dev-run
dev-run: dev-env
	@docker compose up -d --build
	@docker compose logs -f auth0-mock
