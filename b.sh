echo "Building client..."
go build -o bin/client main/client.go

echo "Building MAC service..."
GOARCH="arm64" GOOS="darwin" go build -o bin/serviceapp main/service.go
echo "Building Linux service..."
GOARCH="amd64" GOOS="linux" go build -o bin/service main/service.go
echo "Building Windows service..."
GOARCH="amd64" GOOS="windows" go build -o bin/service.exe main/service.go

echo "Building MAC referral redeem service..."
GOARCH="arm64" GOOS="darwin" go build -o bin/referralsapp referrals/main.go
echo "Building Linux referral redeem service..."
GOARCH="amd64" GOOS="linux" go build -o bin/referrals referrals/main.go
echo "Building Windows referral redeem service..."
GOARCH="amd64" GOOS="windows" go build -o bin/referrals.exe referrals/main.go
