echo "Building client..."
go build -o bin/client client/main.go

echo "Building MAC service..."
GOARCH="arm64" GOOS="darwin" go build -o bin/serviceapp backend/main.go
echo "Building Linux service..."
GOARCH="amd64" GOOS="linux" go build -o bin/service backend/main.go
echo "Building Windows service..."
GOARCH="amd64" GOOS="windows" go build -o bin/service.exe backend/main.go
