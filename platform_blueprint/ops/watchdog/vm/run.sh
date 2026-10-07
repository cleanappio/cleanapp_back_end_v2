#!/usr/bin/env bash
set -euo pipefail

DIR="${CLEANAPP_WATCHDOG_DIR:-${HOME}/cleanapp_watchdog}"
LOG="${DIR}/watchdog.log"
STATUS="${DIR}/status.json"
SECRETS="${DIR}/secrets.env"

lockdir="${CLEANAPP_WATCHDOG_LOCK_DIR:-/tmp/cleanapp_watchdog_lock}"
if ! mkdir "${lockdir}" 2>/dev/null; then
  exit 0
fi
trap 'rmdir "${lockdir}" 2>/dev/null || true' EXIT

ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Check each child explicitly. A brace group followed by || disables Bash
# errexit inside that group, so a failed check must not fall through to OK.
rc=0
failed_check=""
{
  echo ""
  echo "== ${ts} watchdog run =="

  if [[ -f "${SECRETS}" ]]; then
    # optional, VM-local only
    # shellcheck disable=SC1090
    source "${SECRETS}"
  fi

  for check in rabbitmq_ensure.sh human_queue.py smoke_local.sh email_pipeline.sh ingestion_pipeline.sh backup_freshness.sh; do
    if [[ "${check}" == "human_queue.py" ]]; then
      python3 "${DIR}/${check}" || rc=$?
    else
      "${DIR}/${check}" || rc=$?
    fi
    if [[ "${rc}" != "0" ]]; then
      failed_check="${check}"
      echo "FAIL check=${check} rc=${rc}"
      break
    fi
  done
  # Recovery may create work: never run it after a failed prerequisite.
  if [[ "${rc}" == "0" && -x "${DIR}/golden_path.sh" ]]; then
    "${DIR}/golden_path.sh" || { rc=$?; failed_check="golden_path.sh"; }
  fi
} >>"${LOG}" 2>&1

if [[ "${rc}" == "0" ]]; then
  echo "{\"last_ok\":\"${ts}\",\"last_fail\":\"\",\"last_error\":\"\"}" > "${STATUS}"
  # Always attempt to refresh the public status artifact; never fail the run due to this.
  if [[ -x "${DIR}/public_status.sh" ]]; then
    "${DIR}/public_status.sh" || true
  fi
  echo "OK" >>"${LOG}"
else
  err_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "{\"last_ok\":\"\",\"last_fail\":\"${err_ts}\",\"last_error\":\"check=${failed_check} rc=${rc}\"}" > "${STATUS}" || true
  if [[ -x "${DIR}/public_status.sh" ]]; then
    "${DIR}/public_status.sh" || true
  fi
  echo "FAIL rc=${rc}" >>"${LOG}" 2>&1

  # Optional webhook alert. Expected payload: { "text": "..." }.
  # Prefer dedicated watchdog URL; fallback to shared observability webhook.
  webhook_url="${CLEANAPP_WATCHDOG_WEBHOOK_URL:-${CLEANAPP_ALERT_WEBHOOK_URL:-}}"
  if [[ -n "${webhook_url}" ]]; then
    curl -fsS -H "content-type: application/json" -X POST "${webhook_url}" \
      -d "{\"text\":\"[cleanapp watchdog] FAIL check=${failed_check} rc=${rc} at ${err_ts} on $(hostname)\"}" >/dev/null 2>&1 || true
  fi
  exit "${rc}"
fi
