echo "Buiding API server docker image..."
cp ../bin/service ./
docker build . -t ${DOCKER_PREFIX}/cleanappserver:${DOCKER_LABEL}
# docker push ${DOCKER_PREFIX}/cleanappserver
