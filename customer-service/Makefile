# Makefile for CleanApp Customer Service

# Variables
APP_NAME = cleanapp-customer-service
DOCKER_IMAGE = cleanapp/customer-service
VERSION = 1.0.0

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean
GOTEST = $(GOCMD) test
GOGET = $(GOCMD) get
GOMOD = $(GOCMD) mod
BINARY_NAME = main
BINARY_UNIX = $(BINARY_NAME)_unix

# Build the application
build:
	$(GOBUILD) -o $(BINARY_NAME) -v

# Build for Linux
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BINARY_UNIX) -v

# Run the application
run:
	$(GOBUILD) -o $(BINARY_NAME) -v
	@if [ -f .env ]; then \
		export $(cat .env | grep -v '^\#' | xargs) && ./$(BINARY_NAME); \
	else \
		./$(BINARY_NAME); \
	fi

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Download dependencies
deps:
	$(GOMOD) download

# Tidy dependencies
tidy:
	$(GOMOD) tidy

# Run linter
lint:
	golangci-lint run

# Format code
fmt:
	$(GOCMD) fmt ./...

# Run security check
security:
	gosec ./...

# Docker commands
docker-build:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

docker-run:
	docker run -d \
		-p 8080:8080 \
		--name $(APP_NAME) \
		--env-file .env \
		$(DOCKER_IMAGE):latest

docker-stop:
	docker stop $(APP_NAME)
	docker rm $(APP_NAME)

# Docker Compose commands
compose-up:
	docker-compose up -d

compose-down:
	docker-compose down

compose-logs:
	docker-compose logs -f

# Database migrations (placeholder for future implementation)
migrate-up:
	@echo "Running database migrations..."
	@echo "Migrations are automatically run on service startup"
	@echo "To add new migrations, edit database/schema.go"

migrate-down:
	@echo "Rolling back database migrations..."
	@echo "Not implemented - add rollback logic if needed"

migrate-status:
	@echo "Checking migration status..."
	@echo "Connect to MySQL and run: SELECT * FROM cleanapp.schema_migrations;"

# Development helpers
dev:
	air -c .air.toml

# Generate mocks for testing
mocks:
	mockgen -source=database/service.go -destination=mocks/service_mock.go -package=mocks

# API documentation
docs:
	swag init -g main.go

# Health check
health:
	curl -f http://localhost:8080/api/v3/health || exit 1

# Full setup
setup: deps docker-build compose-up
	@echo "Waiting for services to start..."
	@sleep 10
	@make health
	@echo "Setup complete!"

# Help
help:
	@echo "Available targets:"
	@echo "  build          - Build the application"
	@echo "  build-linux    - Build for Linux"
	@echo "  run            - Build and run the application"
	@echo "  clean          - Remove build artifacts"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage"
	@echo "  deps           - Download dependencies"
	@echo "  tidy           - Tidy dependencies"
	@echo "  lint           - Run linter"
	@echo "  fmt            - Format code"
	@echo "  security       - Run security check"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  docker-stop    - Stop Docker container"
	@echo "  compose-up     - Start services with Docker Compose"
	@echo "  compose-down   - Stop services with Docker Compose"
	@echo "  compose-logs   - View Docker Compose logs"
	@echo "  dev            - Run with hot reload (requires air)"
	@echo "  docs           - Generate API documentation"
	@echo "  health         - Check service health"
	@echo "  setup          - Full setup with Docker"
	@echo "  help           - Show this help message"

.PHONY: build build-linux run clean test test-coverage deps tidy lint fmt security \
        docker-build docker-run docker-stop compose-up compose-down compose-logs \
        migrate-up migrate-down migrate-status dev mocks docs health setup help
