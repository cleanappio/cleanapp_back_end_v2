#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

crates=(
  report-tags
  report-fast-renderer
  report-listener-v4
  replier-twitter
)

for c in "${crates[@]}"; do
  if [[ ! -f "$c/Cargo.toml" ]]; then
    echo "missing Cargo.toml: $c" >&2
    exit 1
  fi
done

for c in "${crates[@]}"; do
  echo "== cargo fmt --check: $c =="
  cargo fmt --check --manifest-path "$c/Cargo.toml"
done

echo "OK: cargo fmt"

