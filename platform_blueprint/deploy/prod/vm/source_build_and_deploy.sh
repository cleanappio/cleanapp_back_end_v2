#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"
# shellcheck source=./source_build_map.sh
source "${SCRIPT_DIR}/source_build_map.sh"

HOST="${HOST:-deployer@34.122.15.16}"
REMOTE_BUILD_ROOT="${REMOTE_BUILD_ROOT:-/home/deployer/build_src}"
PROJECT_NAME="${PROJECT_NAME:-cleanup-mysql-v2}"
CLOUD_REGION="${CLOUD_REGION:-us-central1}"
REF="${REF:-HEAD}"
RUN_GO_MIGRATIONS="${RUN_GO_MIGRATIONS:-1}"
KEEP_REMOTE_SOURCE="${KEEP_REMOTE_SOURCE:-0}"
DRY_RUN="${DRY_RUN:-0}"
SOURCE_SERVICES_ENV="${SOURCE_SERVICES:-}"
EXTRA_DEPLOY_SERVICES_ENV="${DEPLOY_SERVICES:-}"

usage() {
  cat >&2 <<USAGE
usage: $0 [source-service ...]

Build from an exact git ref on the prod VM, promote each resulting image to :prod,
then deploy via digest pins.

Inputs:
  SOURCE_SERVICES   space-separated source service directories (or pass as args)
  DEPLOY_SERVICES   optional extra compose service names to restart
  HOST              default: deployer@34.122.15.16
  REF               git ref to stage on the VM (default: HEAD)
  RUN_GO_MIGRATIONS default: 1
  DRY_RUN           default: 0

Example:
  HOST=deployer@34.122.15.16 SOURCE_SERVICES="report-listener customer-service" \
    $0
USAGE
  exit 2
}

collect_source_services() {
  local -a out=()
  if [[ $# -gt 0 ]]; then
    out=("$@")
  elif [[ -n "${SOURCE_SERVICES_ENV}" ]]; then
    # shellcheck disable=SC2206
    out=(${SOURCE_SERVICES_ENV})
  fi
  if [[ ${#out[@]} -eq 0 ]]; then
    usage
  fi
  printf '%s\n' "${out[@]}"
}

dedupe_words() {
  awk '!seen[$0]++ && NF'
}

commit_sha="$(cd "${ROOT_DIR}" && git rev-parse --verify "${REF}^{commit}")"
short_sha="${commit_sha:0:12}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
remote_stage_dir="${REMOTE_BUILD_ROOT}/cleanapp_back_end_v2_${short_sha}_${timestamp}"

source_services=()
while IFS= read -r line; do
  source_services+=("${line}")
done < <(collect_source_services "$@")

deploy_services=()
for svc in "${source_services[@]}"; do
  if [[ ! -d "${ROOT_DIR}/${svc}" ]]; then
    echo "ERROR: unknown source service directory: ${svc}" >&2
    exit 3
  fi
  if ! mapped="$(compose_services_for_source_service "${svc}" 2>/dev/null || true)"; then
    echo "ERROR: no compose mapping registered for source service ${svc}" >&2
    exit 3
  fi
  if [[ -n "${mapped}" ]]; then
    # shellcheck disable=SC2206
    deploy_services+=( ${mapped} )
  fi
  if [[ ! -x "${ROOT_DIR}/${svc}/build_image.sh" ]]; then
    echo "ERROR: missing executable build_image.sh in ${svc}" >&2
    exit 3
  fi
 done

if [[ -n "${EXTRA_DEPLOY_SERVICES_ENV}" ]]; then
  # shellcheck disable=SC2206
  deploy_services+=( ${EXTRA_DEPLOY_SERVICES_ENV} )
fi

deduped_deploy_services=()
while IFS= read -r line; do
  deduped_deploy_services+=("${line}")
done < <(printf '%s\n' "${deploy_services[@]}" | dedupe_words)
deploy_services=("${deduped_deploy_services[@]}")

echo "== source build + prod deploy =="
echo "ref=${REF} (${short_sha})"
echo "host=${HOST}"
echo "source_services=${source_services[*]}"
echo "deploy_services=${deploy_services[*]:-<none>}"
echo "remote_stage_dir=${remote_stage_dir}"

if [[ "${DRY_RUN}" == "1" ]]; then
  exit 0
fi

ssh "${HOST}" "rm -rf '${remote_stage_dir}' && mkdir -p '${remote_stage_dir}'"
(
  cd "${ROOT_DIR}"
  git archive --format=tar "${commit_sha}"
) | ssh "${HOST}" "tar -xf - -C '${remote_stage_dir}'"

remote_source_services="${source_services[*]}"
ssh "${HOST}" "REMOTE_STAGE_DIR='${remote_stage_dir}' PROJECT_NAME='${PROJECT_NAME}' CLOUD_REGION='${CLOUD_REGION}' SOURCE_SERVICES='${remote_source_services}' bash -s" <<'REMOTE'
set -euo pipefail

cd "${REMOTE_STAGE_DIR}"
gcloud config set project "${PROJECT_NAME}" >/dev/null

for svc in ${SOURCE_SERVICES}; do
  echo "== build from source: ${svc} =="
  cd "${REMOTE_STAGE_DIR}/${svc}"
  CLOUDSDK_CONFIG="${CLOUDSDK_CONFIG:-/home/deployer/.config/gcloud}" ./build_image.sh -e dev
  build_version="$(awk -F= '$1=="BUILD_VERSION"{print $2}' .version)"
  docker_image="$(sed -n 's/^DOCKER_IMAGE="\(cleanapp-docker-repo\/[^\"]*\)"/\1/p' build_image.sh | head -n1)"
  if [[ -z "${docker_image}" || -z "${build_version}" ]]; then
    echo "ERROR: could not determine image metadata for ${svc}" >&2
    exit 4
  fi
  docker_tag="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${docker_image}"
  echo "== promote to :prod: ${docker_tag}:${build_version} =="
  gcloud artifacts docker tags add "${docker_tag}:${build_version}" "${docker_tag}:prod"
done
REMOTE

if [[ "${RUN_GO_MIGRATIONS}" == "1" && ${#deploy_services[@]} -gt 0 ]]; then
  HOST="${HOST}" REMOTE_SOURCE_DIR="${remote_stage_dir}" "${SCRIPT_DIR}/run_go_migrations_remote.sh" "${deploy_services[@]}"
fi

if [[ ${#deploy_services[@]} -gt 0 ]]; then
  HOST="${HOST}" RUN_GO_MIGRATIONS=0 SERVICES="${deploy_services[*]}" "${SCRIPT_DIR}/deploy_with_digests.sh"
else
  echo "WARN: no mapped compose services to restart; built and promoted images only" >&2
fi

if [[ "${KEEP_REMOTE_SOURCE}" != "1" ]]; then
  ssh "${HOST}" "rm -rf '${remote_stage_dir}'"
fi

echo "OK: source-built ref ${short_sha} and deployed pinned services"
