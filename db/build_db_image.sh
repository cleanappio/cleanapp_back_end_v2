echo "Buiding MySQL DB docker image..."
docker build . -t ${DOCKER_PREFIX}/cleanappdb:${DOCKER_LABEL}
# docker push ${DOCKER_PREFIX}/cleanappdb
