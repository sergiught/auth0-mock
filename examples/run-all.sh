#!/usr/bin/env bash
# Starts a throwaway mock from the built binary and runs each quickstart example against it.
set -euo pipefail

BIN="${AUTH0_MOCK_BIN:-bin/auth0-mock}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for port in 8443 8080; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "==> ERROR: localhost:$port is already in use. Stop the listener and re-run." >&2
    exit 1
  fi
done

TLS_DIR=$(mktemp -d)
TLS_CACHE_DIR="$TLS_DIR" "$BIN" >"$TLS_DIR/mock.log" 2>&1 &
MOCK_PID=$!
trap 'kill "$MOCK_PID" 2>/dev/null || true; wait "$MOCK_PID" 2>/dev/null || true; rm -rf "$TLS_DIR"' EXIT

ready=0
for _ in $(seq 1 50); do
  if curl -sf --cacert "$TLS_DIR/tls.crt" https://localhost:8443/healthz >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.2
done
if [ "$ready" -ne 1 ]; then
  echo "ERROR: mock did not become ready within 10s. Log:" >&2
  cat "$TLS_DIR/mock.log" >&2
  exit 1
fi

export AUTH0_MOCK_URL="https://localhost:8443/"
export AUTH0_MOCK_CA="$TLS_DIR/tls.crt"

echo "==> curl quickstart"
"$ROOT/examples/http/quickstart.sh"
