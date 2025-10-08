#!/bin/bash

set -euo pipefail

echo "Building email-fetcher docker image..."

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

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-email-fetcher-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

# Create .version if it doesn't exist and bump in dev to ensure fresh digests
if [ ! -f .version ]; then
  echo "BUILD_VERSION=1.0.0" > .version
fi
. .version
if [ "${OPT}" == "dev" ]; then
  BUILD=$(echo ${BUILD_VERSION} | cut -f 3 -d ".")
  VER=$(echo ${BUILD_VERSION} | cut -f 1,2 -d ".")
  BUILD=$((${BUILD} + 1))
  BUILD_VERSION="${VER}.${BUILD}"
  echo "BUILD_VERSION=${BUILD_VERSION}" > .version
fi

CURRENT_PROJECT=$(gcloud config get project)
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

echo "Submitting Cloud Build for ${DOCKER_TAG}:${BUILD_VERSION} ..."
gcloud builds submit --region=${CLOUD_REGION} --tag ${DOCKER_TAG}:${BUILD_VERSION}

echo "Tagging ${DOCKER_TAG}:${BUILD_VERSION} as ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

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


