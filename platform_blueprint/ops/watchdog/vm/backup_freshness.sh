#!/usr/bin/env bash
set -euo pipefail

LOG_FILE="${BACKUP_LOG_FILE:-/home/deployer/backups/backup.log}"
MAX_AGE_HOURS="${BACKUP_MAX_AGE_HOURS:-30}"

if [[ ! -f "${LOG_FILE}" ]]; then
  echo "[backup] missing log file: ${LOG_FILE}" >&2
  exit 1
fi

last_complete_line="$(grep -E "INFO backup complete env=prod" "${LOG_FILE}" | tail -n 1 || true)"
if [[ -z "${last_complete_line}" ]]; then
  echo "[backup] no successful prod backup completion record found in ${LOG_FILE}" >&2
  exit 1
fi

last_ts="$(echo "${last_complete_line}" | awk '{print $1}')"
last_epoch="$(date -u -d "${last_ts}" +%s 2>/dev/null || true)"
if [[ -z "${last_epoch}" ]]; then
  echo "[backup] could not parse backup timestamp: ${last_ts}" >&2
  exit 1
fi

now_epoch="$(date -u +%s)"
age_seconds=$((now_epoch - last_epoch))
max_age_seconds=$((MAX_AGE_HOURS * 3600))

if (( age_seconds > max_age_seconds )); then
  echo "[backup] stale backup: age_seconds=${age_seconds} max_age_seconds=${max_age_seconds} last_ts=${last_ts}" >&2
  exit 1
fi

echo "[backup] OK: age_seconds=${age_seconds} last_ts=${last_ts}"

