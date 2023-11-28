echo "Buiding docker image..."
cp ../bin/service ./
docker build . -t ibnazer/cleanappserver:1.6
# docker push ibnazer/cleanappserver
