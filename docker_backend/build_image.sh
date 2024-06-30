echo "Buiding backend server docker image..."

if [ "$(basename $(pwd))" != "docker_backend" ]; then
  echo "The build image should be run from \"docker_backend\" directory."
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

echo "Building binary..."
test -f service && rm -f service

pushd ../
GOARCH="amd64" GOOS="linux" go build -o docker_backend/service backend/main.go
popd

CLOUD_REGION="us-central1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-service-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

echo "Building and pushing docker image..."
gcloud builds submit \
  --region=${CLOUD_REGION} \
  --tag ${DOCKER_TAG}:${BUILD_VERSION}

echo "Tagging Docker image as current ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

rm -f service