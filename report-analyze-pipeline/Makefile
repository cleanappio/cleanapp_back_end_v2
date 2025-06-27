.PHONY: build run test clean deps env-create env-show

# Build the application
build:
	go build -o report-analyze-pipeline .

# Run the application with .env file support
run:
	@if [ -f .env ]; then \
		echo "Loading environment from .env file..."; \
		set -a; source .env; set +a; go run main.go; \
	else \
		echo "No .env file found. Running with system environment variables..."; \
		go run main.go; \
	fi

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f report-analyze-pipeline

# Download dependencies
deps:
	go mod download
	go mod tidy

# Create .env template
env-create:
	@echo "Creating .env template..."
	@echo "# Database Configuration" > .env.template
	@echo "DB_HOST=localhost" >> .env.template
	@echo "DB_PORT=3306" >> .env.template
	@echo "DB_USER=server" >> .env.template
	@echo "DB_PASSWORD=secret_app" >> .env.template
	@echo "DB_NAME=cleanapp" >> .env.template
	@echo "" >> .env.template
	@echo "# Server Configuration" >> .env.template
	@echo "PORT=8080" >> .env.template
	@echo "" >> .env.template
	@echo "# OpenAI Configuration" >> .env.template
	@echo "OPENAI_API_KEY=your_openai_api_key" >> .env.template
	@echo "OPENAI_MODEL=gpt-4o" >> .env.template
	@echo "" >> .env.template
	@echo "# Analysis Configuration" >> .env.template
	@echo "ANALYSIS_INTERVAL=30s" >> .env.template
	@echo "MAX_RETRIES=3" >> .env.template
	@echo "ANALYSIS_PROMPT=\"What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, give a probability that there is a litter or hazard on a photo and a severity level from 0.0 to 1.0.\"" >> .env.template
	@echo "" >> .env.template
	@echo "# Logging" >> .env.template
	@echo "LOG_LEVEL=info" >> .env.template
	@echo ".env.template created successfully!"
	@echo "Copy .env.template to .env and update the values:"
	@echo "cp .env.template .env"

# Show .env template
env-show:
	@if [ -f .env.template ]; then \
		echo "=== .env.template ==="; \
		cat .env.template; \
	else \
		echo "No .env.template found. Run 'make env-create' to create one."; \
	fi

# Build Docker image
docker-build:
	docker build -t report-analyze-pipeline .

# Run with Docker
docker-run:
	docker run -p 8080:8080 \
		-e DB_HOST=localhost \
		-e DB_PORT=3306 \
		-e DB_USER=server \
		-e DB_PASSWORD=secret_app \
		-e DB_NAME=cleanapp \
		-e OPENAI_API_KEY=your_openai_api_key \
		report-analyze-pipeline 