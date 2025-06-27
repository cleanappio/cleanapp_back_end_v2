.PHONY: build run test clean docker-build docker-run docker-stop help

# Default target
help:
	@echo "Available commands:"
	@echo "  build        - Build the application"
	@echo "  run          - Run the application locally"
	@echo "  test         - Run tests"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run with Docker Compose"
	@echo "  docker-stop  - Stop Docker Compose services"
	@echo "  deps         - Download dependencies"

# Build the application
build:
	go build -o report-listener .

# Run the application locally
run:
	go run main.go

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f report-listener
	go clean

# Download dependencies
deps:
	go mod download
	go mod tidy

# Build Docker image
docker-build:
	docker build -t report-listener .

# Run with Docker Compose
docker-run:
	docker-compose up -d

# Stop Docker Compose services
docker-stop:
	docker-compose down

# Show logs
logs:
	docker-compose logs -f report-listener

# Health check
health:
	curl -s http://localhost:8080/health | jq .

# API health check
api-health:
	curl -s http://localhost:8080/api/v3/reports/health | jq .

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Install dependencies for development
dev-deps:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest 