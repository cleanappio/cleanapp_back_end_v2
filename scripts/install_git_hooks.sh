#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOKS_SRC="$ROOT_DIR/scripts/git-hooks"
HOOKS_DST="$ROOT_DIR/.git/hooks"

if [[ ! -d "$HOOKS_DST" ]]; then
  echo "No .git/hooks directory found (are you in a git repo?)" >&2
  exit 1
fi

install_hook() {
  local name="$1"
  local src="$HOOKS_SRC/$name"
  local dst="$HOOKS_DST/$name"

  if [[ ! -f "$src" ]]; then
    echo "missing hook source: $src" >&2
    exit 1
  fi

  cp "$src" "$dst"
  chmod +x "$dst"
  echo "installed $name"
}

install_hook pre-commit

