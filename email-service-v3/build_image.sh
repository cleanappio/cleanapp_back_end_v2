#!/bin/bash

set -euo pipefail

echo "Building email-service-v3 docker image..."

OPT=""
DEPLOY="0"
SSH_KEYFILE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    -e|--env)
      OPT="$2"; shift 2;;
    -d|--deploy)
      DEPLOY="1"; shift 1;;
    --ssh-keyfile)
      SSH_KEYFILE="$2"; shift 2;;
    *) echo "Unknown opt: $1"; exit 1;;
  esac
done

if [ -z "${OPT}" ]; then
  echo "Usage: $0 -e|--env <dev|prod> [-d|--deploy] [--ssh-keyfile <path>]"; exit 1;
fi

BUILD_SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../scripts/build/deprecate_prod_tagging.sh
source "${BUILD_SCRIPT_DIR}/../scripts/build/deprecate_prod_tagging.sh"
warn_deprecated_prod_tagging "${OPT}" "$(basename "${BUILD_SCRIPT_DIR}")"


CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-email-service-v3-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

CURRENT_PROJECT=$(gcloud config get project)
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

echo "Submitting Cloud Build..."
"${BUILD_SCRIPT_DIR}/../scripts/build/submit_rust_service_with_common.sh" \
  "${BUILD_SCRIPT_DIR}" \
  "${CLOUD_REGION}" \
  "${DOCKER_TAG}:${OPT}" \
  "${PROJECT_NAME}"

echo "Tagging as latest ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${OPT} ${DOCKER_TAG}:${OPT}

echo "Done."

if [ "${DEPLOY}" == "1" ]; then
  echo "Deploying to ${OPT} via setup.sh..."
  pushd ../setup >/dev/null
  if [ -n "${SSH_KEYFILE}" ]; then
    ./setup.sh -e ${OPT} --ssh-keyfile ${SSH_KEYFILE}
  else
    ./setup.sh -e ${OPT}
  fi
  popd >/dev/null
fi
