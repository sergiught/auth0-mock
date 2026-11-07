#!/usr/bin/env bash
# Runs the Testcontainers examples against the mock image.
# Usage: run-all.sh [suite...]   suite is one of: go node python java (default: every suite that exists)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

if [ "$#" -gt 0 ]; then
  suites=("$@")
else
  suites=()
  for s in go node python java; do
    [ -d "$HERE/$s" ] && suites+=("$s")
  done
fi

if [ "${#suites[@]}" -eq 0 ]; then
  echo "ERROR: no suites to run" >&2
  exit 1
fi

# Fail on a typo before the slow image build.
for s in "${suites[@]}"; do
  case "$s" in
    go|node|python|java) ;;
    *) echo "ERROR: unknown suite: $s (expected go, node, python or java)" >&2; exit 1 ;;
  esac
done

if [ -z "${AUTH0_MOCK_IMAGE:-}" ]; then
  AUTH0_MOCK_IMAGE=auth0-mock:testcontainers-local
  echo "==> Building $AUTH0_MOCK_IMAGE"
  docker build -t "$AUTH0_MOCK_IMAGE" "$ROOT"
fi
export AUTH0_MOCK_IMAGE

run_suite() {
  case "$1" in
    go) (cd "$HERE/go" && go test -count=1 ./...) ;;
    node) (cd "$HERE/node" && npm ci --no-audit --no-fund && npm test) ;;
    *) echo "unsupported suite: $1" >&2; return 1 ;;
  esac
}

failed=()
for s in "${suites[@]}"; do
  echo "==> $s"
  run_suite "$s" || failed+=("$s")
done

if [ "${#failed[@]}" -gt 0 ]; then
  echo "FAILED: ${failed[*]}" >&2
  exit 1
fi
