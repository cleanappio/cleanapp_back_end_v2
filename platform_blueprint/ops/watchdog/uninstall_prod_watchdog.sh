#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-}"

if [[ -z "${HOST}" ]]; then
  echo "usage: HOST=deployer@<prod-vm> $0" >&2
  exit 2
fi

echo "[watchdog] uninstalling from ${HOST}"

ssh "${HOST}" "bash -s" <<'EOF'
set -euo pipefail

tmp=\"$(mktemp)\"
crontab -l 2>/dev/null | grep -v \"cleanapp_watchdog/run.sh\" > \"$tmp\" || true
crontab \"$tmp\" || true
rm -f \"$tmp\"

rm -rf ~/cleanapp_watchdog

echo \"[watchdog] removed\"
EOF
