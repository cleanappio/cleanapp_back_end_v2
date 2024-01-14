echo "Buiding API server docker image..."

if [ "$(basename $(pwd))" != "docker" ]; then
  echo "The build image should be run from \"docker\" directory."
  exit 1
fi

. .version
echo "Running docker build for version ${BUILD_VERSION}"

echo "Building binary..."
test -f service && rm -f service

pushd ../
GOARCH="amd64" GOOS="linux" go build -o docker/service main/service.go
popd

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-service-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

echo "Building and pushing docker image..."
gcloud builds submit \
  --region=${CLOUD_REGION} \
  --tag ${DOCKER_TAG}:${BUILD_VERSION}

echo "Tagging Docker image as live..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:live
