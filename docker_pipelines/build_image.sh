echo "Buiding pipelines docker image..."

if [ "$(basename $(pwd))" != "docker_pipelines" ]; then
  echo "The build image should be run from \"docker_pipelines\" directory."
  exit 1
fi

# Choose the environment
PS3="Please choose the environment: "
options=("local" "dev" "prod" "quit")
select OPT in "${options[@]}"
do
  case ${OPT} in
    "local")
        echo "Using local environment"
        break
        ;;
    "dev")
        echo "Using dev environment"
        break
        ;;
    "prod")
        echo "Using prod environment"
        break
        ;;
    "quit")
        exit
        ;;
    *) echo "invalid option $REPLY";;
  esac
done

. .version
echo "Running docker build for version ${BUILD_VERSION}"

set -e

echo "Building binary..."
test -f pipelines && rm -f pipelines

pushd ../
GOARCH="amd64" GOOS="linux" go build -o docker_pipelines/pipelines pipelines/main.go
popd

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-pipelines-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

CURRENT_PROJECT=$(gcloud config get project)
echo ${CURRENT_PROJECT}
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

echo "Building and pushing docker image..."
gcloud builds submit \
  --region=${CLOUD_REGION} \
  --tag ${DOCKER_TAG}:${BUILD_VERSION}

echo "Tagging Docker image as current ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

rm -f pipelines
