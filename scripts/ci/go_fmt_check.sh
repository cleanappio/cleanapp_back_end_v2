#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

# Exclude vendored code and generated/vendor snapshots.
find . -type f -name '*.go' \
  -not -path './vendor/*' \
  -not -path '*/vendor/*' \
  -not -path './xray/*' \
  -print0 \
  | xargs -0 gofmt -l >"$tmp"

if [[ -s "$tmp" ]]; then
  echo "gofmt needed on:" >&2
  cat "$tmp" >&2
  exit 1
fi

echo "OK: gofmt"

