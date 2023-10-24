echo "Buiding docker image..."
pushd docker
cp ../bin/service ./
docker build . -t ibnazer/cleanappserver
# docker push ibnazer/cleanappserver
popd
