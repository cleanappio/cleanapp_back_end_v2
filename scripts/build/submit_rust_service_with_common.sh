#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: $0 <service_dir> <region> <image_ref> <project_name>" >&2
  exit 2
fi

SERVICE_DIR="$1"
REGION="$2"
IMAGE_REF="$3"
PROJECT_NAME="$4"

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

rsync -a \
  --exclude target \
  --exclude .git \
  --exclude .DS_Store \
  "${SERVICE_DIR}/" "${TMP_DIR}/"

if [[ -f "${SERVICE_DIR}/Cargo.toml" ]] && grep -q '\.\./rust-common' "${SERVICE_DIR}/Cargo.toml"; then
  rsync -a "${ROOT_DIR}/rust-common/" "${TMP_DIR}/rust-common/"
  perl -0pi -e 's#\.\./rust-common#rust-common#g' "${TMP_DIR}/Cargo.toml"
fi

if [[ -f "${TMP_DIR}/buildinfo.vars" ]]; then
  # shellcheck disable=SC1091
  source "${TMP_DIR}/buildinfo.vars"
fi

if [[ -z "${CLEANAPP_BUILD_VERSION:-}" ]]; then
  if [[ -f "${TMP_DIR}/.version" ]]; then
    CLEANAPP_BUILD_VERSION="$(awk -F= '$1=="BUILD_VERSION"{print $2}' "${TMP_DIR}/.version" | tr -d "\"'[:space:]")"
  elif [[ -f "${TMP_DIR}/build_version.txt" ]]; then
    CLEANAPP_BUILD_VERSION="$(tr -d "\"'[:space:]" < "${TMP_DIR}/build_version.txt")"
  fi
fi

if [[ -z "${CLEANAPP_BUILD_VERSION:-}" ]]; then
  CLEANAPP_BUILD_VERSION="dev"
fi

if [[ -n "${CLEANAPP_GIT_SHA_OVERRIDE:-}" ]]; then
  CLEANAPP_GIT_SHA="${CLEANAPP_GIT_SHA_OVERRIDE}"
fi

if [[ -n "${CLEANAPP_BUILD_TIME_OVERRIDE:-}" ]]; then
  CLEANAPP_BUILD_TIME="${CLEANAPP_BUILD_TIME_OVERRIDE}"
fi

if [[ -z "${CLEANAPP_BUILD_TIME:-}" ]]; then
  CLEANAPP_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

cat > "${TMP_DIR}/buildinfo.vars" <<EOF
CLEANAPP_BUILD_VERSION=${CLEANAPP_BUILD_VERSION}
CLEANAPP_GIT_SHA=${CLEANAPP_GIT_SHA:-}
CLEANAPP_BUILD_TIME=${CLEANAPP_BUILD_TIME}
EOF

gcloud builds submit "${TMP_DIR}" \
  --project="${PROJECT_NAME}" \
  --region="${REGION}" \
  --tag="${IMAGE_REF}"
