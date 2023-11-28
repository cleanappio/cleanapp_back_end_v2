echo "Buiding docker images..."
export CLEANAPP_VERSION="1.6"
pushd docker
./build_server_image.sh
popd

pushd db
./build_db_image.sh
popd
