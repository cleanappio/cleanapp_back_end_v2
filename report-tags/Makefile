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
	cargo build --release

# Run the application locally (loads .env file if it exists)
run:
	@if [ -f .env ]; then \
		echo "Loading environment from .env file..."; \
		export $$(grep -v '^#' .env | xargs) && cargo run; \
	else \
		echo "No .env file found. Running with default environment variables..."; \
		echo "Run 'make env-setup' to create a .env file."; \
		cargo run; \
	fi

# Run tests
test:
	cargo test

# Clean build artifacts
clean:
	cargo clean

# Download dependencies
deps:
	cargo fetch
	cargo update

# Create .env file from template
env-setup:
	@if [ ! -f .env ]; then \
		echo "Creating .env file from template..."; \
		cp .env.example .env 2>/dev/null || echo "Creating default .env file..."; \
		echo "# Report Tags Service Environment Configuration" > .env; \
		echo "DB_HOST=localhost" >> .env; \
		echo "DB_PORT=3306" >> .env; \
		echo "DB_USER=server" >> .env; \
		echo "DB_PASSWORD=secret_app" >> .env; \
		echo "DB_NAME=cleanapp" >> .env; \
		echo "PORT=8083" >> .env; \
		echo "RUST_LOG=info" >> .env; \
		echo "MAX_TAG_FOLLOWS=200" >> .env; \
		echo ".env file created successfully!"; \
		echo "Please edit .env file with your actual configuration."; \
	else \
		echo ".env file already exists. Skipping creation."; \
	fi

# Show example .env file
env-example:
	@echo "# Report Tags Service Environment Configuration"
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
	@echo "PORT=8083"
	@echo ""
	@echo "# Logging"
	@echo "RUST_LOG=info"
	@echo ""
	@echo "# Tag Following Limit"
	@echo "MAX_TAG_FOLLOWS=200"

# Build Docker image
docker-build:
	docker build -t report-tags .

# Run with Docker Compose
docker-run:
	docker-compose up -d

# Stop Docker Compose services
docker-stop:
	docker-compose down

# Show logs
logs:
	docker-compose logs -f report-tags

# Health check
health:
	curl -s http://localhost:8083/health | jq .

# Format code
fmt:
	cargo fmt

# Lint code
lint:
	cargo clippy

# Install dependencies for development
dev-deps:
	cargo install cargo-watch

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
	export $$(grep -v '^#' $(ENV_FILE) | xargs) && cargo run

# Development mode with hot reload (requires cargo-watch)
dev:
	@if command -v cargo-watch >/dev/null 2>&1; then \
		if [ -f .env ]; then \
			export $$(grep -v '^#' .env | xargs) && cargo watch -x run; \
		else \
			cargo watch -x run; \
		fi; \
	else \
		echo "cargo-watch not found. Install with: cargo install cargo-watch"; \
		echo "Or run: make run"; \
	fi
