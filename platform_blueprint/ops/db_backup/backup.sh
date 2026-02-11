#!/usr/bin/env bash
# CleanApp MySQL full backup -> GCS (prod/dev)
# - Streams mysqldump -> gzip -> gsutil (no large local temp files)
# - Writes metadata JSON alongside backup
# - Weekly pin (Sundays UTC): copies current object to weekly/<ISO_WEEK>/
set -euo pipefail

ENV=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -e|--env)
      ENV="$2"; shift 2;;
    *)
      echo "Usage: $0 -e <dev|prod>" >&2
      exit 2;;
  esac
done

if [[ -z "${ENV}" ]]; then
  echo "Usage: $0 -e <dev|prod>" >&2
  exit 2
fi

case "${ENV}" in
  dev|prod) ;;
  *) echo "Invalid env: ${ENV} (expected dev|prod)" >&2; exit 2;;
esac

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*"; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || { log "ERROR missing command: $1" >&2; exit 1; }; }

need_cmd gcloud
need_cmd gsutil
need_cmd gzip

if ! sudo -n true 2>/dev/null; then
  log "ERROR sudo requires a password; cannot run docker exec" >&2
  exit 1
fi

SECRET_SUFFIX="$(echo "${ENV}" | tr '[:lower:]' '[:upper:]')"
BUCKET="gs://cleanapp_mysql_backup_${ENV}"
CURRENT_KEY="${BUCKET}/current/cleanapp_all.sql.gz"
CURRENT_META_KEY="${BUCKET}/current/cleanapp_all.metadata.json"

log "INFO backup start env=${ENV} bucket=${BUCKET}"

MYSQL_ROOT_PASSWORD="$(gcloud secrets versions access latest --secret="MYSQL_ROOT_PASSWORD_${SECRET_SUFFIX}" 2>/dev/null)" || {
  log "ERROR failed to read MySQL root password from Secret Manager" >&2
  exit 1
}

if ! sudo docker ps --format '{{.Names}}' | grep -qx cleanapp_db; then
  log "ERROR cleanapp_db container not running" >&2
  exit 1
fi

if command -v pigz >/dev/null 2>&1; then
  COMPRESS=(pigz -1)
else
  COMPRESS=(gzip -1)
fi

started_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
started_epoch="$(date +%s)"

log "INFO mysqldump stream start"
sudo docker exec -e MYSQL_PWD="${MYSQL_ROOT_PASSWORD}" -i cleanapp_db sh -lc \
  'exec mysqldump -uroot \
    --all-databases \
    --single-transaction \
    --quick \
    --lock-tables=false \
    --routines --events --triggers \
    --hex-blob \
    --set-gtid-purged=OFF' \
  | "${COMPRESS[@]}" \
  | gsutil -q -o GSUtil:parallel_composite_upload_threshold=150M cp - "${CURRENT_KEY}"

finished_epoch="$(date +%s)"
finished_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
duration_s=$((finished_epoch - started_epoch))

size_bytes="$(gsutil ls -l "${CURRENT_KEY}" | awk 'NR==1{print $1}')"
size_bytes="${size_bytes:-0}"

log "INFO capturing row counts"
reports_count="$(sudo docker exec -e MYSQL_PWD="${MYSQL_ROOT_PASSWORD}" -i cleanapp_db sh -lc 'mysql -uroot -N -e "SELECT COUNT(*) FROM cleanapp.reports" 2>/dev/null' | tr -d '\r' | tail -n 1 || true)"
analysis_count="$(sudo docker exec -e MYSQL_PWD="${MYSQL_ROOT_PASSWORD}" -i cleanapp_db sh -lc 'mysql -uroot -N -e "SELECT COUNT(*) FROM cleanapp.report_analysis" 2>/dev/null' | tr -d '\r' | tail -n 1 || true)"
reports_count="${reports_count:-0}"
analysis_count="${analysis_count:-0}"
counts_json="{\"reports\":${reports_count},\"report_analysis\":${analysis_count}}"

meta_tmp="/tmp/cleanapp_all.metadata.$$.$RANDOM.json"
cat >"${meta_tmp}" <<META
{
  "env": "${ENV}",
  "object": "${CURRENT_KEY}",
  "started_utc": "${started_ts}",
  "finished_utc": "${finished_ts}",
  "duration_seconds": ${duration_s},
  "size_bytes": ${size_bytes},
  "row_counts": ${counts_json}
}
META

gsutil -q cp "${meta_tmp}" "${CURRENT_META_KEY}"
rm -f "${meta_tmp}" || true

log "INFO backup uploaded object=${CURRENT_KEY} size_bytes=${size_bytes} duration_s=${duration_s}"

if [[ "$(date -u +%u)" == "7" ]]; then
  week="$(date -u +%G-W%V)"
  weekly_key="${BUCKET}/weekly/${week}/cleanapp_all.sql.gz"
  weekly_meta_key="${BUCKET}/weekly/${week}/cleanapp_all.metadata.json"
  log "INFO weekly pin start week=${week}"
  gsutil -q cp "${CURRENT_KEY}" "${weekly_key}"
  gsutil -q cp "${CURRENT_META_KEY}" "${weekly_meta_key}"
  log "INFO weekly pin done weekly_object=${weekly_key}"
fi

log "INFO backup complete env=${ENV}"

