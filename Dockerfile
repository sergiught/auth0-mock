# syntax=docker/dockerfile:1.28
#
# Local-dev Dockerfile for auth0-mock — builds from source with the Go
# toolchain so `docker compose up --build` is fast on every source edit.
# For published images the release pipeline uses the slimmer
# `Dockerfile.release`, which is fed a pre-built binary by goreleaser.

FROM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS build
WORKDIR /src

# Cache deps separately from source so a code-only change doesn't re-download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/auth0-mock ./cmd/api

# --- runtime image -----------------------------------------------------------

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
LABEL org.opencontainers.image.title="auth0-mock"
LABEL org.opencontainers.image.description="Drop-in mock of Auth0's Authentication and Management APIs."
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/sergiught/auth0-mock"

# ca-certificates so outbound HTTPS (e.g. JWKS over TLS) verifies real CAs.
# wget so docker-compose / orchestrator healthchecks can hit /healthz without
# a separate probe binary (~250 KB extra).
RUN apk add --no-cache ca-certificates tini wget

# TLS_CACHE_DIR=/data/tls needs a directory that UID 65534 can write. A named
# volume mounted here copies this owner on first use; without it the volume
# is root-owned and the mock can't save its cert.
RUN mkdir -p /data/tls && chown 65534:65534 /data/tls

COPY --from=build /out/auth0-mock /usr/local/bin/auth0-mock

EXPOSE 8080 8443

# Bind on all interfaces inside the container so `docker run -p 8080:8080`
# can reach the listener. The binary defaults to 127.0.0.1 for local dev safety;
# this override re-enables the container use case without leaking that bind
# choice to bare-metal runs. The mock refuses a non-loopback bind unless
# ADMIN_ALLOW_REMOTE=true, so users must pass -e ADMIN_ALLOW_REMOTE=true.
ENV HTTP_ADDR=0.0.0.0:8080 \
    HTTPS_ADDR=0.0.0.0:8443

# Run as nobody (UID/GID 65534) — both 8080 and 8443 are unprivileged ports.
USER 65534:65534

# tini is PID 1 so SIGTERM is delivered cleanly to the binary, which then
# triggers our graceful shutdown path.
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/auth0-mock"]
