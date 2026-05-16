# syntax=docker/dockerfile:1.7
#
# Production-grade single-stage build for auth0-mock.
# Used for both local development (`docker compose up`) and image publishing.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache deps separately from source so a code-only change doesn't re-download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/auth0-mock ./cmd/api

# --- runtime image -----------------------------------------------------------

FROM alpine:3.20
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
