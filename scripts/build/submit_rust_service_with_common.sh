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

gcloud builds submit "${TMP_DIR}" \
  --project="${PROJECT_NAME}" \
  --region="${REGION}" \
  --tag="${IMAGE_REF}"
