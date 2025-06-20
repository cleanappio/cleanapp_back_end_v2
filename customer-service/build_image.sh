echo "Buiding customer service docker image..."

OPT=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Choose the environment if not specified
if [ -z "${OPT}" ]; then
  PS3="Please choose the environment: "
  options=("dev" "prod" "quit")
  select OPT in "${options[@]}"
  do
    case ${OPT} in
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
fi

test -d target && rm -rf target

. .version

# Increment version build number
if [ "${OPT}" == "dev" ]; then
  BUILD=$(echo ${BUILD_VERSION} | cut -f 3 -d ".")
  VER=$(echo ${BUILD_VERSION} | cut -f 1,2 -d ".")
  BUILD=$((${BUILD} + 1))
  BUILD_VERSION="${VER}.${BUILD}"
  echo "BUILD_VERSION=${BUILD_VERSION}" > .version
  cargo set-version ${BUILD_VERSION}
fi

echo "Running docker build for version ${BUILD_VERSION}"

set -e

CLOUD_REGION="us-east1"
PROJECT_NAME="cleanup-mysql-v2"
DOCKER_IMAGE="cleanapp-docker-repo/cleanapp-customer-service-image"
DOCKER_TAG="${CLOUD_REGION}-docker.pkg.dev/${PROJECT_NAME}/${DOCKER_IMAGE}"

CURRENT_PROJECT=$(gcloud config get project)
echo ${CURRENT_PROJECT}
if [ "${PROJECT_NAME}" != "${CURRENT_PROJECT}" ]; then
  gcloud auth login
  gcloud config set project ${PROJECT_NAME}
fi

if [ "${OPT}" == "dev" ]; then
  echo "Building and pushing docker image..."
  gcloud builds submit \
    --region=${CLOUD_REGION} \
    --substitutions=_TAG=${DOCKER_TAG}:${BUILD_VERSION}
fi

echo "Tagging Docker image as current ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}