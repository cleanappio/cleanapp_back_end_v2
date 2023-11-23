echo "Buiding docker image..."
pushd docker
cp ../bin/service ./
docker build . -t ibnazer/cleanappserver:1.5
# docker push ibnazer/cleanappserver
popd
