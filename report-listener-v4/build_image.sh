#!/usr/bin/env bash
set -euo pipefail

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

echo "Building report-listener-v4 docker image..."

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-report-listener-v4-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

CURRENT_PROJECT=$(gcloud config get project)
echo ${CURRENT_PROJECT}
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

# Use Cloud Build to produce a reproducible build, tag with version if present
if [ -f .version ]; then
  . .version
else
  echo "BUILD_VERSION=1.0.0" > .version
  . .version
fi

if [ "${OPT}" == "dev" ]; then
  BUILD=$(echo ${BUILD_VERSION} | cut -f 3 -d ".")
  VER=$(echo ${BUILD_VERSION} | cut -f 1,2 -d ".")
  BUILD=$((${BUILD} + 1))
  BUILD_VERSION="${VER}.${BUILD}"
  echo "BUILD_VERSION=${BUILD_VERSION}" > .version
fi

echo "Running Cloud Build for version ${BUILD_VERSION}" 

cleanup_buildinfo() { rm -f buildinfo.vars; }
trap cleanup_buildinfo EXIT

GIT_SHA="$(git rev-parse --short=12 HEAD 2>/dev/null || true)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > buildinfo.vars <<EOF
CLEANAPP_BUILD_VERSION=${BUILD_VERSION}
CLEANAPP_GIT_SHA=${GIT_SHA}
CLEANAPP_BUILD_TIME=${BUILD_TIME}
EOF

gcloud builds submit \
  --region=${CLOUD_REGION} \
  --tag=${DOCKER_TAG}:${BUILD_VERSION}

echo "Tagging Docker image as current ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

echo "report-listener-v4 docker image build completed successfully!"

if [ -n "${SSH_KEYFILE}" ]; then
  pushd ../setup >/dev/null
  ./setup.sh -e ${OPT} --ssh-keyfile ${SSH_KEYFILE}
  popd >/dev/null
fi


