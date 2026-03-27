#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
REMOTE_STAGE_DIR="${REMOTE_STAGE_DIR:-/home/deployer/build_src/report_listener_backfill_stage}"
REMOTE_SOURCE_DIR="${REMOTE_SOURCE_DIR:-}"
LOCAL_ROOT="${LOCAL_ROOT:-$(cd "$(dirname "$0")/../../../.." && pwd)}"
GO_IMAGE="${GO_IMAGE:-golang:1.24-alpine}"
SINCE_HOURS="${SINCE_HOURS:-96}"
LIMIT="${LIMIT:-250}"
DRY_RUN="${DRY_RUN:-0}"
REPUBLISH="${REPUBLISH:-1}"

if [[ -z "${REMOTE_SOURCE_DIR}" ]]; then
  stage_dir="$(mktemp -d)"
  cleanup() {
    rm -rf "${stage_dir}"
  }
  trap cleanup EXIT

  mkdir -p "${stage_dir}/go-common"
  cp -R "${LOCAL_ROOT}/go-common/." "${stage_dir}/go-common/"
  mkdir -p "${stage_dir}/report-listener"
  cp -R "${LOCAL_ROOT}/report-listener/." "${stage_dir}/report-listener/"

  ssh "${HOST}" "rm -rf '${REMOTE_STAGE_DIR}' && mkdir -p '${REMOTE_STAGE_DIR}'"
  tar --no-xattrs -czf - -C "${stage_dir}" . | ssh "${HOST}" "tar xzf - -C '${REMOTE_STAGE_DIR}'"
else
  REMOTE_STAGE_DIR="${REMOTE_SOURCE_DIR}"
fi

ssh "${HOST}" "bash -s -- '${REMOTE_STAGE_DIR}' '${GO_IMAGE}' '${SINCE_HOURS}' '${LIMIT}' '${DRY_RUN}' '${REPUBLISH}'" <<'REMOTE'
set -euo pipefail

REMOTE_STAGE_DIR="${1}"
GO_IMAGE="${2}"
SINCE_HOURS="${3}"
LIMIT="${4}"
DRY_RUN="${5}"
REPUBLISH="${6}"

env_file="$(mktemp)"
cleanup() {
  rm -f "${env_file}"
}
trap cleanup EXIT

if [[ -f /home/deployer/.env ]]; then
  awk 'index($0,"=")>1 && $1 !~ /^#/ {print}' /home/deployer/.env > "${env_file}"
fi
sudo -n docker inspect cleanapp_report_listener --format "{{range .Config.Env}}{{println .}}{{end}}" >> "${env_file}"
awk -F= '!seen[$1]++' "${env_file}" > "${env_file}.dedup"
mv "${env_file}.dedup" "${env_file}"

network_name="$(
  sudo -n docker inspect cleanapp_report_listener --format '{{range $k,$v := .NetworkSettings.Networks}}{{println $k}}{{end}}' 2>/dev/null | head -n1
)"
if [[ -z "${network_name}" ]]; then
  network_name="$(
    sudo -n docker network ls --format '{{.Name}}' | awk '/_default$/ {print; exit}'
  )"
fi
if [[ -z "${network_name}" ]]; then
  echo "ERROR: could not determine docker network for cleanapp_report_listener" >&2
  exit 1
fi

echo "== report-listener digital share image backfill =="
echo "since_hours=${SINCE_HOURS} limit=${LIMIT} dry_run=${DRY_RUN} republish=${REPUBLISH}"

extra_flags=()
if [[ "${DRY_RUN}" == "1" ]]; then
  extra_flags+=("-dry-run")
fi
if [[ "${REPUBLISH}" == "0" ]]; then
  extra_flags+=("-republish=false")
fi

sudo -n docker run --rm \
  --network "${network_name}" \
  --env-file "${env_file}" \
  -v "${REMOTE_STAGE_DIR}:/workspace" \
  -w "/workspace/report-listener" \
  "${GO_IMAGE}" \
  sh -lc "apk add --no-cache git >/dev/null 2>&1 && /usr/local/go/bin/go run ./cmd/backfill-digital-share-images -since-hours '${SINCE_HOURS}' -limit '${LIMIT}' ${extra_flags[*]}"
REMOTE

if [[ -z "${REMOTE_SOURCE_DIR}" ]]; then
  ssh "${HOST}" "rm -rf '${REMOTE_STAGE_DIR}'"
fi
