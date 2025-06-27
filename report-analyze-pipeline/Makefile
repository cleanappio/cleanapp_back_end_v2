.PHONY: build run test clean deps

# Build the application
build:
	go build -o report-analyze-pipeline .

# Run the application
run:
	go run main.go

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