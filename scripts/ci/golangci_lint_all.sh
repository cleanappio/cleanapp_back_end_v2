#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint not found in PATH" >&2
  exit 2
fi

mapfile -t mods < <(
  find . -name go.mod -type f \
    -not -path './vendor/*' \
    -not -path '*/vendor/*' \
    -not -path './xray/*' \
    -print \
  | sed 's|/go.mod$||' \
  | sort
)

for d in "${mods[@]}"; do
  echo "== golangci-lint: $d =="
  (cd "$d" && golangci-lint run --timeout=5m)
done

echo "OK: golangci-lint"

