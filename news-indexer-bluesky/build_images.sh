#!/bin/bash

echo "Building news-indexer-bluesky docker images..."

OPT=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"; shift 2;;
    *) echo "Unknown option: $1"; exit 1;;
  esac
done

if [ -z "${OPT}" ]; then
  echo "Usage: $0 -e|--env <dev|prod>"
  exit 1
fi

case ${OPT} in
  dev|prod) ;; 
  *) echo "env must be dev or prod"; exit 1;;
esac

test -d target && rm -rf target

if [ ! -f .version ]; then
  echo "1.0.0" > .version
fi
BUILD_VERSION=$(cat .version)

if [ "${OPT}" == "dev" ]; then
  BUILD=$(echo ${BUILD_VERSION} | cut -f 3 -d ".")
  VER=$(echo ${BUILD_VERSION} | cut -f 1,2 -d ".")
  BUILD=$((${BUILD} + 1))
  BUILD_VERSION="${VER}.${BUILD}"
  echo "${BUILD_VERSION}" > .version
fi

set -e

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
REPO_PATH="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/cleanapp-docker-repo"

TAG_INDEXER="${REPO_PATH}/cleanapp-bluesky-indexer-image:${BUILD_VERSION}"
TAG_ANALYZER="${REPO_PATH}/cleanapp-bluesky-analyzer-image:${BUILD_VERSION}"
TAG_SUBMITTER="${REPO_PATH}/cleanapp-bluesky-submitter-image:${BUILD_VERSION}"

CURRENT_PROJECT=$(gcloud config get project)
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

echo "Building all bluesky images (version ${BUILD_VERSION})..."
gcloud builds submit \
  --region=${CLOUD_REGION} \
  --config=cloudbuild.all.yaml \
  --substitutions=_TAG_INDEXER=${TAG_INDEXER},_TAG_ANALYZER=${TAG_ANALYZER},_TAG_SUBMITTER=${TAG_SUBMITTER} \
  .

echo "Tagging images as ${OPT}..."
gcloud artifacts docker tags add ${TAG_INDEXER} ${REPO_PATH}/cleanapp-bluesky-indexer-image:${OPT}
gcloud artifacts docker tags add ${TAG_ANALYZER} ${REPO_PATH}/cleanapp-bluesky-analyzer-image:${OPT}
gcloud artifacts docker tags add ${TAG_SUBMITTER} ${REPO_PATH}/cleanapp-bluesky-submitter-image:${OPT}

echo "Done."
