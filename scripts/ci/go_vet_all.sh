#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

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
  echo "== go vet: $d =="
  (cd "$d" && go vet ./...)
done

echo "OK: go vet"

