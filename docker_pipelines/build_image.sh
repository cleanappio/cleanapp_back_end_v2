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

cleanup_buildinfo() { rm -f buildinfo.vars; }
trap cleanup_buildinfo EXIT

GIT_SHA="$(git rev-parse --short=12 HEAD 2>/dev/null || true)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > buildinfo.vars <<EOF
CLEANAPP_BUILD_VERSION=${BUILD_VERSION}
CLEANAPP_GIT_SHA=${GIT_SHA}
CLEANAPP_BUILD_TIME=${BUILD_TIME}
EOF

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
pushd ../ >/dev/null
gcloud builds submit \
  --region=${CLOUD_REGION} \
  --substitutions=_TAG=${DOCKER_TAG}:${BUILD_VERSION} \
  --config=docker_pipelines/cloudbuild.yaml
popd >/dev/null

echo "Tagging Docker image as current ${OPT}..."
gcloud artifacts docker tags add ${DOCKER_TAG}:${BUILD_VERSION} ${DOCKER_TAG}:${OPT}

if [ -n "${SSH_KEYFILE}" ]; then
  pushd ../setup
  ./setup.sh -e ${OPT} --ssh-keyfile ${SSH_KEYFILE}
  popd
fi
