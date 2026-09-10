#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
for service in report-analyze-pipeline report-listener report-tags report-fast-renderer; do
  dest="$root/.ci-build/$service"
  mkdir -p "$dest"
  rsync -a --delete --exclude target --exclude .git "$root/$service/" "$dest/"
  if [[ -f "$dest/go.mod" ]]; then
    rsync -a "$root/go-common/" "$dest/go-common/"
  fi
  if [[ -f "$dest/Cargo.toml" ]] && grep -q '../rust-common' "$dest/Cargo.toml"; then
    rsync -a "$root/rust-common/" "$dest/rust-common/"
    sed -i.bak 's#../rust-common#rust-common#g' "$dest/Cargo.toml"
    rm "$dest/Cargo.toml.bak"
  fi
done
