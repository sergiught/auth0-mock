---
title: Installation
description: Install and run auth0-mock from a release, from source, with go install, in a live-reload loop or in Docker.
sidebar:
  order: 1
---

## From a release (recommended)

```bash
# latest stable
curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh | bash

# pinned version
curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh | bash -s v0.227.0

# install to a user-writable dir (no sudo)
BIN_DIR="$HOME/.local/bin" bash <(curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh)
```

The script downloads the goreleaser-built archive, verifies its sha256 against
the release's `checksums.txt`, and installs the binary as `auth0-mock`. Source
is at [`install.sh`](https://github.com/sergiught/auth0-mock/blob/main/install.sh) — review before piping to bash if that bothers you.

## From source

```bash
make build && ./bin/auth0-mock
```

## Via `go install`

```bash
go install github.com/sergiught/auth0-mock/cmd/api@latest
$(go env GOPATH)/bin/api -version    # installs as `api`, not `auth0-mock`
```

`install.sh` and `make build` are still the recommended paths because they
stamp the binary with `version` / `commit` / `date` (visible via
`auth0-mock -version`) and install it as `auth0-mock`. `go install` is here
for Go developers who'd rather rebuild from source every time and don't
mind the cmd-package naming.

## Live-reload dev loop (`air`)

Sub-second rebuild on every save under `cmd/` or `internal/`, no docker, no bind-mounts, no flakiness:

```bash
make watch     # installs air into ./bin on first run
```

## Docker

To start the mock from your tests or CI, see [Testcontainers and CI](/auth0-mock/guides/testcontainers-and-ci/).

```bash
docker compose up -d --build
docker compose logs -f auth0-mock
```

> **Changed:** the mock now refuses to listen on `0.0.0.0` (as it does inside Docker) unless `ADMIN_ALLOW_REMOTE=true`. `.env.example` sets it for the compose stack; add `-e ADMIN_ALLOW_REMOTE=true` to `docker run`, and add it to an existing `.env`.

`docker compose` builds from the local dev `Dockerfile` (Go toolchain + source) for fast `--build` iteration. The release pipeline uses a separate slim `Dockerfile.release` (binary-only, fed by goreleaser) — `ghcr.io/sergiught/auth0-mock:X.Y.Z` and `docker.io/sergiught/auth0-mock:X.Y.Z` are what hits Docker Hub.

