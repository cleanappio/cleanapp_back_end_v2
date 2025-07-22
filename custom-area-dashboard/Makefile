.PHONY: build run test clean docker-build docker-run

# Build the service
build:
	go build -o custom-area-dashboard-service .

# Run the service
run:
	@if [ -f .env ]; then \
		set -a; source .env; set +a; go run main.go; \
	else \
		go run main.go; \
	fi

# Run the service in development mode
run-dev:
	@echo "Loading environment variables from .env file..."
	@if [ -f .env ]; then \
		set -a; source .env; set +a; go run main.go; \
	else \
		echo "Warning: .env file not found, using system environment variables"; \
		go run main.go; \
	fi

# Test the service
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f custom-area-dashboard-service

# Build Docker image
docker-build:
	docker build -t custom-area-dashboard .

# Run Docker container
docker-run:
	docker run -p 8080:8080 custom-area-dashboard

# Download dependencies
deps:
	go mod tidy
	go mod download 