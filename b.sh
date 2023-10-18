echo "Building client..."
go build main/client.go
mv client bin/

echo "Building MAC service..."
GOARCH="arm64" GOOS="darwin" go build -o service.app main/service.go
echo "Building Linux service..."
GOARCH="amd64" GOOS="linux" go build -o service main/service.go
echo "Building Windows service..."
GOARCH="amd64" GOOS="windows" go build main/service.go
mv service* bin/
