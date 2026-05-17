#!/usr/bin/env bash
#
# auth0-mock installer
#
# Downloads a goreleaser-published archive from a GitHub release, verifies
# its sha256 against the release's checksums.txt, extracts the binary, and
# installs it. Default install dir is /usr/local/bin (may need sudo); set
# BIN_DIR to install somewhere you own without sudo (e.g. ~/.local/bin).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh | bash -s v0.227.0
#   BIN_DIR=$HOME/.local/bin bash install.sh
#
# Variables (env or first positional arg for VERSION):
#   VERSION    Specific tag (e.g. v0.227.0). Default: latest.
#   BIN_DIR    Install directory. Default: /usr/local/bin.
#   GH_REPO    Repository to fetch from. Default: sergiught/auth0-mock.

set -euo pipefail

REPO="${GH_REPO:-sergiught/auth0-mock}"
VERSION="${1:-${VERSION:-latest}}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"

say() { printf '==> %s\n' "$*"; }
die() { printf 'auth0-mock install: %s\n' "$*" >&2; exit 1; }

# ── platform detection ─────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *) die "unsupported OS '$OS' (this script supports linux and darwin)" ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH=x86_64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) die "unsupported architecture '$ARCH' (this script supports x86_64 and arm64)" ;;
esac

# ── tooling ────────────────────────────────────────────────────────────────
need() { command -v "$1" >/dev/null 2>&1 || die "required tool '$1' not on PATH"; }
need curl
need tar
if command -v sha256sum >/dev/null 2>&1; then
  SHASUM="sha256sum"
else
  command -v shasum >/dev/null 2>&1 || die "neither sha256sum nor shasum is installed"
  SHASUM="shasum -a 256"
fi

# ── version resolution (no GitHub API call → no rate limit) ────────────────
if [ "$VERSION" = "latest" ]; then
  say "resolving latest release for $REPO"
  # /releases/latest redirects to /releases/tag/vX.Y.Z; we follow it and read
  # the resolved URL instead of hitting the rate-limited REST API. Drop
  # `-f` here so a 404 on a release-less repo yields our friendly `die`
  # below instead of curl's terse exit-22 message under `set -e`.
  RESOLVED=$(curl -sSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPO}/releases/latest") || \
    die "could not reach https://github.com/${REPO}/releases/latest — check network / repo path"
  VERSION="${RESOLVED##*/tag/}"
  [ -n "$VERSION" ] && [ "$VERSION" != "$RESOLVED" ] || \
    die "could not resolve latest release (does ${REPO} have any published releases yet?)"
fi
case "$VERSION" in
  v*) ;;
  *)  die "VERSION must look like vMAJOR.MINOR.PATCH (got '$VERSION')" ;;
esac
say "installing auth0-mock $VERSION for $OS/$ARCH"

# ── download + verify ─────────────────────────────────────────────────────
ARCHIVE="auth0-mock_${VERSION#v}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/${VERSION}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

say "downloading $ARCHIVE"
curl -fsSL "${BASE}/${ARCHIVE}"        -o "${TMP}/${ARCHIVE}"
curl -fsSL "${BASE}/checksums.txt"     -o "${TMP}/checksums.txt"

say "verifying sha256"
( cd "$TMP" && grep " $ARCHIVE\$" checksums.txt | $SHASUM -c - ) \
  || die "sha256 verification failed for $ARCHIVE"

# ── install ────────────────────────────────────────────────────────────────
say "extracting"
tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"
[ -x "${TMP}/auth0-mock" ] || die "binary 'auth0-mock' not found in archive"

INSTALL_PATH="${BIN_DIR}/auth0-mock"

# Use sudo only if we can't write the dir directly. mkdir -p the dir first
# so that "is it writable" is meaningful even on fresh systems.
if [ ! -d "$BIN_DIR" ]; then
  if mkdir -p "$BIN_DIR" 2>/dev/null; then :; else
    say "$BIN_DIR does not exist and is not writable — using sudo"
    sudo mkdir -p "$BIN_DIR"
  fi
fi

if [ -w "$BIN_DIR" ]; then
  install -m 0755 "${TMP}/auth0-mock" "$INSTALL_PATH"
else
  say "$BIN_DIR not writable by current user — escalating to sudo"
  sudo install -m 0755 "${TMP}/auth0-mock" "$INSTALL_PATH"
fi

say "installed to $INSTALL_PATH"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "note: $BIN_DIR is not on your PATH; add it to your shell profile or run with the full path" ;;
esac
say "done. run: auth0-mock"
