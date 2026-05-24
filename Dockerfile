# syntax=docker/dockerfile:1.24
#
# Local-dev Dockerfile for auth0-mock — builds from source with the Go
# toolchain so `docker compose up --build` is fast on every source edit.
# For published images the release pipeline uses the slimmer
# `Dockerfile.release`, which is fed a pre-built binary by goreleaser.

FROM golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS build
WORKDIR /src

# Cache deps separately from source so a code-only change doesn't re-download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/auth0-mock ./cmd/api

# --- runtime image -----------------------------------------------------------

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
LABEL org.opencontainers.image.title="auth0-mock"
LABEL org.opencontainers.image.description="Drop-in mock of Auth0's Authentication and Management APIs."
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/sergiught/auth0-mock"

# ca-certificates so outbound HTTPS (e.g. JWKS over TLS) verifies real CAs.
# wget so docker-compose / orchestrator healthchecks can hit /healthz without
# a separate probe binary (~250 KB extra).
RUN apk add --no-cache ca-certificates tini wget

COPY --from=build /out/auth0-mock /usr/local/bin/auth0-mock

EXPOSE 8080 8443

# Bind on all interfaces inside the container so `docker run -p 8080:8080`
# can reach the listener. The binary defaults to 127.0.0.1 for local dev safety;
# this override re-enables the container use case without leaking that bind
# choice to bare-metal runs.
ENV HTTP_ADDR=0.0.0.0:8080 \
    HTTPS_ADDR=0.0.0.0:8443

# Run as nobody (UID/GID 65534) — both 8080 and 8443 are unprivileged ports.
USER 65534:65534

# tini is PID 1 so SIGTERM is delivered cleanly to the binary, which then
# triggers our graceful shutdown path.
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/auth0-mock"]
