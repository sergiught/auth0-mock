BINARIES_DIR = $(CURDIR)/bin
BINARY_NAME = auth0-mock

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

.PHONY: lint
lint:
	@go vet ./...

.PHONY: dev-env
dev-env:
	@cp -n .env.example .env || true

# Install air (live-reload) into ./bin if not already present.
$(BINARIES_DIR)/air:
	@echo "==> Installing air into $(BINARIES_DIR)"
	@GOBIN=$(BINARIES_DIR) go install github.com/air-verse/air@latest

# Native dev loop: rebuild + restart on every save under ./cmd or ./internal.
# No docker, no bind-mounts — sub-second iteration.
.PHONY: watch
watch: dev-env $(BINARIES_DIR)/air
	@$(BINARIES_DIR)/air

.PHONY: dev-run
dev-run: dev-env
	@docker compose up -d --build
	@docker compose logs -f auth0-mock
