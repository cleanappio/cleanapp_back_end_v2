.PHONY: help build run test clean docker-build docker-run

# Default target
help:
	@echo "Available targets:"
	@echo "  build        - Build the Go binary"
	@echo "  run          - Run the service locally"
	@echo "  test         - Test database connection"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run with Docker Compose"
	@echo "  deps         - Download Go dependencies"

# Build the Go binary
build:
	@echo "Building GDPR Process Service..."
	go build -o main .

# Run the service locally
run: build
	@echo "Running GDPR Process Service..."
	./main

# Test database connection
test:
	@echo "Testing database connection..."
	./test_connection.sh

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f main
	go clean

# Download Go dependencies
deps:
	@echo "Downloading Go dependencies..."
	go mod tidy
	go mod download

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	./build_image.sh

# Run with Docker Compose
docker-run: docker-build
	@echo "Starting service with Docker Compose..."
	docker-compose up -d

# Stop Docker Compose
docker-stop:
	@echo "Stopping Docker Compose services..."
	docker-compose down

# View logs
logs:
	docker-compose logs -f gdpr-process-service
