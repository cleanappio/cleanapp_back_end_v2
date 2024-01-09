echo "Buiding docker images..."
# Docker images label.
export DOCKER_LABEL="1.6"
# Dockerhub images prefix.
export DOCKER_PREFIX="ibnazer"
pushd docker
./build_server_image.sh
popd

pushd db
./build_db_image.sh
popd
