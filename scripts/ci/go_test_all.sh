#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

mods=()
while IFS= read -r d; do
  mods+=("$d")
done < <(
  find . -name go.mod -type f \
    -not -path './vendor/*' \
    -not -path '*/vendor/*' \
    -not -path './xray/*' \
    -print \
    | sed 's|/go.mod$||' \
    | sort
)

if [[ "${#mods[@]}" -eq 0 ]]; then
  echo "no go modules found" >&2
  exit 1
fi

for d in "${mods[@]}"; do
  echo "== go test: $d =="
  (cd "$d" && go test ./... -count=1 -timeout=5m)
done

echo "OK: go test"
