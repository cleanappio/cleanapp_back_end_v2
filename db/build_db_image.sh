echo "Buiding MySQL DB docker image..."

if [ "$(basename $(pwd))" != "db" ]; then
  echo "The build image should be run from \"db\" directory."
  exit 1
fi

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-db-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

echo "Building and pushing docker image..."
gcloud builds submit \
  --region=${CLOUD_REGION} \
  --tag ${DOCKER_TAG}:live
