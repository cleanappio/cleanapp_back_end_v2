.PHONY: help build run test clean docker-build docker-run docker-stop deps fmt lint

# Default target
help:
	@echo "Brand Dashboard Service - Available commands:"
	@echo "  build        - Build the application"
	@echo "  run          - Run the application locally"
	@echo "  test         - Run tests"
	@echo "  clean        - Clean build artifacts"
	@echo "  deps         - Download dependencies"
	@echo "  fmt          - Format code"
	@echo "  lint         - Run linter"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run with Docker Compose"
	@echo "  docker-stop  - Stop Docker Compose services"

# Build the application
build:
	@echo "Building brand-dashboard..."
	go build -o brand-dashboard .

# Run the application locally
run:
	@echo "Running brand-dashboard..."
	go run main.go

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -cover ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f brand-dashboard
	go clean

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	golangci-lint run

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t brand-dashboard .

# Run with Docker Compose
docker-run:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d

# Stop Docker Compose services
docker-stop:
	@echo "Stopping Docker Compose services..."
	docker-compose down

# View logs
logs:
	docker-compose logs -f brand-dashboard

# Restart service
restart:
	docker-compose restart brand-dashboard

# Health check
health:
	@echo "Checking service health..."
	curl -f http://localhost:8080/health || echo "Service is not healthy"

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/cosmtrek/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Development with hot reload (requires air)
dev:
	@echo "Starting development server with hot reload..."
	air

# Create .env file from example
setup-env:
	@echo "Creating .env file..."
	@if [ ! -f .env ]; then \
		echo "DB_USER=server" > .env; \
		echo "DB_PASSWORD=secret_app" >> .env; \
		echo "DB_HOST=localhost" >> .env; \
		echo "DB_PORT=3306" >> .env; \
		echo "DB_NAME=cleanapp" >> .env; \
		echo "PORT=8080" >> .env; \
		echo "HOST=0.0.0.0" >> .env; \
		echo "AUTH_SERVICE_URL=http://auth-service:8080" >> .env; \
		echo "BRAND_NAMES=coca-cola,redbull,nike,adidas,pepsi,mcdonalds,starbucks,apple,samsung,microsoft" >> .env; \
		echo ".env file created successfully"; \
	else \
		echo ".env file already exists"; \
	fi

# Show service status
status:
	@echo "Service status:"
	@docker-compose ps
	@echo ""
	@echo "Health check:"
	@curl -s http://localhost:8080/health | jq . 2>/dev/null || echo "Service not responding"

# Backup database
backup:
	@echo "Creating database backup..."
	docker exec cleanapp-mysql mysqldump -u server -psecret_app cleanapp > backup_$(shell date +%Y%m%d_%H%M%S).sql

# Restore database
restore:
	@echo "Restoring database from backup..."
	@read -p "Enter backup file name: " backup_file; \
	docker exec -i cleanapp-mysql mysql -u server -psecret_app cleanapp < $$backup_file 