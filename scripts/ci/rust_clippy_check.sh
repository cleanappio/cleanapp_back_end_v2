#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

crates=(
  report-tags
  report-fast-renderer
  report-listener-v4
  replier-twitter
  email-fetcher
)

for c in "${crates[@]}"; do
  echo "== cargo clippy: $c =="
  cargo clippy --manifest-path "$c/Cargo.toml" --all-targets
done

echo "OK: cargo clippy"
