#!/usr/bin/env bash
# Installs the prod DB backup script + cron schedule on the prod VM.
#
# Also sets bucket lifecycle:
# - keep 7 noncurrent versions under current/ (daily versions)
# - keep weekly/ objects for 210 days (~30 weeks)
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
ENV_NAME="${ENV_NAME:-prod}"

if [[ "${ENV_NAME}" != "prod" && "${ENV_NAME}" != "dev" ]]; then
  echo "ENV_NAME must be prod|dev" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

tmp_lifecycle="$(mktemp)"
trap 'rm -f "$tmp_lifecycle"' EXIT

cat >"$tmp_lifecycle" <<EOF
{
  "rule": [
    {
      "action": {"type": "Delete"},
      "condition": {
        "isLive": false,
        "numNewerVersions": 7,
        "matchesPrefix": ["current/"]
      }
    },
    {
      "action": {"type": "Delete"},
      "condition": {
        "age": 210,
        "matchesPrefix": ["weekly/"]
      }
    }
  ]
}
EOF

echo "== install backup script on VM =="
ssh "$HOST" "set -euo pipefail; mkdir -p /home/deployer/backups; true"
scp "$SCRIPT_DIR/backup.sh" "$HOST:/home/deployer/backup.sh"
ssh "$HOST" "set -euo pipefail; chmod +x /home/deployer/backup.sh"

echo "== ensure cron (daily 03:30 UTC) =="
ssh "$HOST" "set -euo pipefail; (crontab -l 2>/dev/null | grep -v '/home/deployer/backup.sh' || true) > /tmp/cron.new; echo '30 3 * * * /home/deployer/backup.sh -e ${ENV_NAME} >> /home/deployer/backups/backup.log 2>&1' >> /tmp/cron.new; crontab /tmp/cron.new; rm -f /tmp/cron.new; crontab -l"

echo "== set bucket lifecycle =="
bucket="gs://cleanapp_mysql_backup_${ENV_NAME}"
gsutil lifecycle set "$tmp_lifecycle" "$bucket"
gsutil lifecycle get "$bucket"

