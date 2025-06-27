.PHONY: build run test clean docker-build docker-run docker-stop help env-setup

# Default target
help:
	@echo "Available commands:"
	@echo "  build        - Build the application"
	@echo "  run          - Run the application locally (loads .env file)"
	@echo "  test         - Run tests"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run with Docker Compose"
	@echo "  docker-stop  - Stop Docker Compose services"
	@echo "  deps         - Download dependencies"
	@echo "  env-setup    - Create .env file from template"
	@echo "  env-example  - Show example .env file"

# Build the application
build:
	go build -o report-listener .

# Run the application locally (loads .env file if it exists)
run:
	@if [ -f .env ]; then \
		echo "Loading environment from .env file..."; \
		export $$(grep -v '^#' .env | xargs) && go run main.go; \
	else \
		echo "No .env file found. Running with default environment variables..."; \
		echo "Run 'make env-setup' to create a .env file."; \
		go run main.go; \
	fi

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

# Create .env file from template
env-setup:
	@if [ ! -f .env ]; then \
		echo "Creating .env file from template..."; \
		cp .env.example .env 2>/dev/null || echo "Creating default .env file..."; \
		echo "# Report Listener Environment Configuration" > .env; \
		echo "DB_HOST=localhost" >> .env; \
		echo "DB_PORT=3306" >> .env; \
		echo "DB_USER=server" >> .env; \
		echo "DB_PASSWORD=secret_app" >> .env; \
		echo "DB_NAME=cleanapp" >> .env; \
		echo "PORT=8080" >> .env; \
		echo "BROADCAST_INTERVAL=1s" >> .env; \
		echo "LOG_LEVEL=info" >> .env; \
		echo ".env file created successfully!"; \
		echo "Please edit .env file with your actual configuration."; \
	else \
		echo ".env file already exists. Skipping creation."; \
	fi

# Show example .env file
env-example:
	@echo "# Report Listener Environment Configuration"
	@echo "# Copy this to .env and modify as needed"
	@echo ""
	@echo "# Database Configuration"
	@echo "DB_HOST=localhost"
	@echo "DB_PORT=3306"
	@echo "DB_USER=server"
	@echo "DB_PASSWORD=secret_app"
	@echo "DB_NAME=cleanapp"
	@echo ""
	@echo "# Server Configuration"
	@echo "PORT=8080"
	@echo ""
	@echo "# Broadcast Configuration"
	@echo "BROADCAST_INTERVAL=1s"
	@echo ""
	@echo "# Logging"
	@echo "LOG_LEVEL=info"

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

# Run with specific environment file
run-env:
	@if [ -z "$(ENV_FILE)" ]; then \
		echo "Usage: make run-env ENV_FILE=path/to/env/file"; \
		exit 1; \
	fi; \
	if [ ! -f "$(ENV_FILE)" ]; then \
		echo "Environment file $(ENV_FILE) not found!"; \
		exit 1; \
	fi; \
	echo "Loading environment from $(ENV_FILE)..."; \
	export $$(grep -v '^#' $(ENV_FILE) | xargs) && go run main.go

# Development mode with hot reload (requires air)
dev:
	@if command -v air >/dev/null 2>&1; then \
		if [ -f .env ]; then \
			export $$(grep -v '^#' .env | xargs) && air; \
		else \
			air; \
		fi; \
	else \
		echo "Air not found. Install with: go install github.com/cosmtrek/air@latest"; \
		echo "Or run: make run"; \
	fi 