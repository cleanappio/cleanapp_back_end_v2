#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-}"
CRON_SCHEDULE="${CRON_SCHEDULE:-*/5 * * * *}"

if [[ -z "${HOST}" ]]; then
  echo "usage: HOST=deployer@<prod-vm> $0" >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VM_DIR="${ROOT_DIR}/vm"

echo "[watchdog] installing on ${HOST}"

tar -C "${VM_DIR}" -czf - rabbitmq_ensure.sh smoke_local.sh backup_freshness.sh golden_path.sh run.sh | \
  ssh "${HOST}" "bash -lc 'set -euo pipefail; mkdir -p ~/cleanapp_watchdog; chmod 700 ~/cleanapp_watchdog; tar -xzf - -C ~/cleanapp_watchdog; chmod 700 ~/cleanapp_watchdog/*.sh'"

# Install/refresh cron entry (idempotent).
qcron="$(printf "%q" "${CRON_SCHEDULE}")"
ssh "${HOST}" "CRON_SCHEDULE=${qcron} bash -s" <<'__REMOTE__'
set -euo pipefail
tmp="$(mktemp)"
crontab -l 2>/dev/null | grep -v "cleanapp_watchdog/run.sh" > "$tmp" || true
echo "${CRON_SCHEDULE} $HOME/cleanapp_watchdog/run.sh" >> "$tmp"
crontab "$tmp"
rm -f "$tmp"
echo "[watchdog] installed cron: ${CRON_SCHEDULE}"
__REMOTE__

echo "[watchdog] installed"
