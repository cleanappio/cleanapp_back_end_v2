#!/bin/bash

echo "Building replier-twitter docker image..."

OPT=""
SSH_KEYFILE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"
      shift 2
      ;;
    "--ssh-keyfile")
      SSH_KEYFILE="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

if [ -z "${OPT}" ]; then
  echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
  exit 1
fi

BUILD_SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../scripts/build/deprecate_prod_tagging.sh
source "${BUILD_SCRIPT_DIR}/../scripts/build/deprecate_prod_tagging.sh"
warn_deprecated_prod_tagging "${OPT}" "$(basename "${BUILD_SCRIPT_DIR}")"


case ${OPT} in
  "dev")
    echo "Using dev environment"
    TARGET_VM_IP="34.132.121.53"
    ;;
  "prod")
    echo "Using prod environment"
    TARGET_VM_IP="34.122.15.16"
    ;;
  *)
    echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
    exit 1
    ;;
esac

test -d target && rm -rf target

# Create .version file if it doesn't exist
if [ ! -f .version ]; then
  echo "BUILD_VERSION=1.0.0" > .version
fi

. .version

# Increment version build number (dev only)
if [ "${OPT}" == "dev" ]; then
  BUILD=$(echo ${BUILD_VERSION} | cut -f 3 -d ".")
  VER=$(echo ${BUILD_VERSION} | cut -f 1,2 -d ".")
  BUILD=$((${BUILD} + 1))
  BUILD_VERSION="${VER}.${BUILD}"
  echo "BUILD_VERSION=${BUILD_VERSION}" > .version
fi

echo "Running docker build for version ${BUILD_VERSION}"

set -e

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-news-replier-twitter-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

CURRENT_PROJECT=$(gcloud config get project)
echo ${CURRENT_PROJECT}
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

if [ "${OPT}" == "dev" ]; then
  echo "Building and pushing docker image..."
  "${BUILD_SCRIPT_DIR}/../scripts/build/submit_rust_service_with_common.sh" \
    "${BUILD_SCRIPT_DIR}" \
    "${CLOUD_REGION}" \
    "${DOCKER_TAG}:${BUILD_VERSION}" \
    "${PROJECT_NAME}"
fi

echo "Tagging Docker image as current ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

echo "replier-twitter docker image build completed successfully!"

if [ -n "${SSH_KEYFILE}" ]; then
  pushd ../setup
  ./setup.sh -e ${OPT} --ssh-keyfile ${SSH_KEYFILE}
  popd
fi

