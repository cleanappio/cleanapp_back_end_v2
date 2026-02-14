#!/usr/bin/env bash
set -euo pipefail

DIR="${HOME}/cleanapp_watchdog"
LOG="${DIR}/watchdog.log"
STATUS="${DIR}/status.json"
SECRETS="${DIR}/secrets.env"

lockdir="/tmp/cleanapp_watchdog_lock"
if ! mkdir "${lockdir}" 2>/dev/null; then
  exit 0
fi
trap 'rmdir "${lockdir}" 2>/dev/null || true' EXIT

ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

{
  echo ""
  echo "== ${ts} watchdog run =="

  if [[ -f "${SECRETS}" ]]; then
    # optional, VM-local only
    # shellcheck disable=SC1090
    source "${SECRETS}"
  fi

  "${DIR}/rabbitmq_ensure.sh"
  "${DIR}/smoke_local.sh"
  "${DIR}/email_pipeline.sh"
  "${DIR}/backup_freshness.sh"
  # Only does work when a report appears stuck; otherwise it is passive.
  if [[ -x "${DIR}/golden_path.sh" ]]; then
    "${DIR}/golden_path.sh"
  fi

  echo "{\"last_ok\":\"${ts}\",\"last_fail\":\"\",\"last_error\":\"\"}" > "${STATUS}"
  echo "OK"
} >>"${LOG}" 2>&1 || {
  rc=$?
  err_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "{\"last_ok\":\"\",\"last_fail\":\"${err_ts}\",\"last_error\":\"rc=${rc}\"}" > "${STATUS}" || true
  echo "FAIL rc=${rc}" >>"${LOG}" 2>&1

  # Optional webhook alert. Expected payload: { "text": "..." }.
  # Prefer dedicated watchdog URL; fallback to shared observability webhook.
  webhook_url="${CLEANAPP_WATCHDOG_WEBHOOK_URL:-${CLEANAPP_ALERT_WEBHOOK_URL:-}}"
  if [[ -n "${webhook_url}" ]]; then
    curl -fsS -H "content-type: application/json" -X POST "${webhook_url}" \
      -d "{\"text\":\"[cleanapp watchdog] FAIL rc=${rc} at ${err_ts} on $(hostname)\"}" >/dev/null 2>&1 || true
  fi
  exit "${rc}"
}
