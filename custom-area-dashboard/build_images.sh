#!/bin/bash

echo "Building custom areas dashboard docker image..."

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

# Increment version build number
if [ "${OPT}" == "dev" ]; then
  BUILD=$(echo ${BUILD_VERSION} | cut -f 3 -d ".")
  VER=$(echo ${BUILD_VERSION} | cut -f 1,2 -d ".")
  BUILD=$((${BUILD} + 1))
  BUILD_VERSION="${VER}.${BUILD}"
  echo "BUILD_VERSION=${BUILD_VERSION}" > .version
fi

echo "Running docker build for version ${BUILD_VERSION}"

set -e

MONTENEGRO_AREAS_GEOJSON_FILE="/app/OSMB-e0b412fe96a2a2c5d8e7eb33454a21d971bea620.geojson"

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"

CURRENT_PROJECT=$(gcloud config get project)
echo ${CURRENT_PROJECT}
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

for DASHBOARD in "montenegro"; do
  case ${DASHBOARD} in
    "montenegro")
      AREAS_GEOJSON_FILE=${MONTENEGRO_AREAS_GEOJSON_FILE}
      CUSTOM_AREA_ADMIN_LEVEL=2
      CUSTOM_AREA_OSM_ID=-53296
      ;;
    *)
      echo "Unknown dashboard: ${DASHBOARD}"
      exit 1
      ;;
  esac
  DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-${DASHBOARD}-custom-area-dashboard-image"
  DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

  cat Dockerfile.template | \
  sed "s/{{AREAS_GEOJSON_FILE}}/${AREAS_GEOJSON_FILE}/" | \
  sed "s/{{CUSTOM_AREA_ADMIN_LEVEL}}/${CUSTOM_AREA_ADMIN_LEVEL}/" | \
  sed "s/{{CUSTOM_AREA_OSM_ID}}/${CUSTOM_AREA_OSM_ID}/" | \
  > Dockerfile

  if [ "${OPT}" == "dev" ]; then
    echo "Building and pushing docker image..."
    gcloud builds submit \
      --region=${CLOUD_REGION} \
      --tag=${DOCKER_TAG}:${BUILD_VERSION}
  fi
  echo "Tagging Docker image as current ${OPT}..."
  gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

  test -f Dockerfile && rm Dockerfile
done

echo "custom area dashboard docker images are built successfully!"

if [ -n "${SSH_KEYFILE}" ]; then
  SETUP_SCRIPT="https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/refs/heads/main/setup/setup.sh"
  
  # Copy deployment script on target VM and run it 
  curl ${SETUP_SCRIPT} | ssh -i ${SSH_KEYFILE} deployer@${TARGET_VM_IP} "cat > deploy.sh && chmod +x deploy.sh && ./deploy.sh -e ${OPT}"
fi
