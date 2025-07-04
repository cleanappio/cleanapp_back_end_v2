echo "Buiding pipelines docker image..."

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

if [ -n "${SSH_KEYFILE}" ]; then
  SETUP_SCRIPT="https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/refs/heads/main/setup/setup.sh"
  
  # Copy deployment script on target VM and run it 
  curl ${SETUP_SCRIPT} | ssh -i ${SSH_KEYFILE} deployer@${TARGET_VM_IP} "cat > deploy.sh && chmod +x deploy.sh && ./deploy.sh -e ${OPT}"
fi
