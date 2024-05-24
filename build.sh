echo "Building MAC service..."
GOARCH="arm64" GOOS="darwin" go build -o bin/serviceapp backend/main.go
echo "Building Linux service..."
GOARCH="amd64" GOOS="linux" go build -o bin/service backend/main.go
echo "Building Windows service..."
GOARCH="amd64" GOOS="windows" go build -o bin/service.exe backend/main.go

echo "Building MAC pipeline service..."
GOARCH="arm64" GOOS="darwin" go build -o bin/pipelinesapp pipelines/main.go
echo "Building Linux pipeline service..."
GOARCH="amd64" GOOS="linux" go build -o bin/pipelines pipelines/main.go
echo "Building Windows pipeline service..."
GOARCH="amd64" GOOS="windows" go build -o bin/pipelines.exe pipelines/main.go
