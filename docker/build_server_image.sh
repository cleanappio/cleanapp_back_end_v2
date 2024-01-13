RUNFROM=$(basename $(pwd))
if [ "${RUNFROM}" != "docker" ]; then
  echo "The script is to be run from the \"<project_root>/docker\" directory."
  exit 1
fi
. ../.env
echo "Buiding API server docker image..."
../b.sh
cp ../bin/service ./
docker build . -t ${DOCKER_PREFIX}/cleanappserver:${DOCKER_LABEL}
docker push ${DOCKER_PREFIX}/cleanappserver:${DOCKER_LABEL}
rm service