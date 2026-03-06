#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
REMOTE_STAGE_DIR="${REMOTE_STAGE_DIR:-/home/deployer/build_src/go_migrations_stage}"
LOCAL_ROOT="${LOCAL_ROOT:-$(cd "$(dirname "$0")/../../../.." && pwd)}"
GO_IMAGE="${GO_IMAGE:-golang:1.24-alpine}"

service_dir_for() {
  case "$1" in
    cleanapp_auth_service) echo "auth-service" ;;
    cleanapp_customer_service) echo "customer-service" ;;
    cleanapp_report_listener) echo "report-listener" ;;
    cleanapp_areas_service) echo "areas-service" ;;
    cleanapp_email_service) echo "email-service" ;;
    cleanapp_report_ownership_service) echo "report-ownership-service" ;;
    cleanapp_report_analyze_pipeline) echo "report-analyze-pipeline" ;;
    cleanapp_report_processor) echo "report-processor" ;;
    cleanapp_gdpr_process_service) echo "gdpr-process-service" ;;
    *) return 1 ;;
  esac
}

services=(
  cleanapp_auth_service
  cleanapp_customer_service
  cleanapp_report_listener
  cleanapp_report_analyze_pipeline
  cleanapp_report_processor
  cleanapp_gdpr_process_service
  cleanapp_areas_service
  cleanapp_email_service
  cleanapp_report_ownership_service
)

if [[ $# -gt 0 ]]; then
  services=("$@")
fi

stage_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${stage_dir}"
}
trap cleanup EXIT

mkdir -p "${stage_dir}/go-common"
cp -R "${LOCAL_ROOT}/go-common/." "${stage_dir}/go-common/"

for service in "${services[@]}"; do
  repo_dir="$(service_dir_for "$service" || true)"
  if [[ -z "${repo_dir}" ]]; then
    echo "WARN: skipping unknown migration service ${service}" >&2
    continue
  fi
  mkdir -p "${stage_dir}/${repo_dir}"
  cp -R "${LOCAL_ROOT}/${repo_dir}/." "${stage_dir}/${repo_dir}/"
done

ssh "${HOST}" "rm -rf '${REMOTE_STAGE_DIR}' && mkdir -p '${REMOTE_STAGE_DIR}'"
tar --no-xattrs -czf - -C "${stage_dir}" . | ssh "${HOST}" "tar xzf - -C '${REMOTE_STAGE_DIR}'"

ssh "${HOST}" "bash -s -- '${REMOTE_STAGE_DIR}' '${GO_IMAGE}' ${services[*]}" <<'REMOTE'
set -euo pipefail
REMOTE_STAGE_DIR="${1}"
GO_IMAGE="${2}"
shift 2

service_dir_for() {
  case "$1" in
    cleanapp_auth_service) echo "auth-service" ;;
    cleanapp_customer_service) echo "customer-service" ;;
    cleanapp_report_listener) echo "report-listener" ;;
    cleanapp_areas_service) echo "areas-service" ;;
    cleanapp_email_service) echo "email-service" ;;
    cleanapp_report_ownership_service) echo "report-ownership-service" ;;
    cleanapp_report_analyze_pipeline) echo "report-analyze-pipeline" ;;
    cleanapp_report_processor) echo "report-processor" ;;
    cleanapp_gdpr_process_service) echo "gdpr-process-service" ;;
    *) return 1 ;;
  esac
}

for service in "$@"; do
  repo_dir="$(service_dir_for "$service" || true)"
  if [[ -z "${repo_dir}" ]]; then
    echo "WARN: skipping unknown migration service ${service}" >&2
    continue
  fi
  env_file="$(mktemp)"
  if [[ -f /home/deployer/.env ]]; then
    awk 'index($0,"=")>1 && $1 !~ /^#/ {print}' /home/deployer/.env > "$env_file"
  fi
  sudo -n docker inspect "$service" --format "{{range .Config.Env}}{{println .}}{{end}}" >> "$env_file"
  awk -F= '!seen[$1]++' "$env_file" > "${env_file}.dedup"
  mv "${env_file}.dedup" "$env_file"
  echo "== remote migrate: ${service} =="
  sudo -n docker run --rm \
    --network "container:${service}" \
    --env-file "$env_file" \
    -v "${REMOTE_STAGE_DIR}:/workspace" \
    -w "/workspace/${repo_dir}" \
    "${GO_IMAGE}" \
    sh -lc 'apk add --no-cache git >/dev/null 2>&1 && /usr/local/go/bin/go run ./cmd/migrate'
  rm -f "$env_file"
  echo
done
REMOTE
