.PHONY: build run test clean docker-build docker-run

# Build the application
build:
	go build -o main .

# Run the application locally
run:
	go run main.go

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f main

# Build Docker image
docker-build:
	docker build -t email-service .

# Run with Docker Compose
docker-run:
	docker-compose up -d

# Stop Docker Compose
docker-stop:
	docker-compose down

# View logs
logs:
	docker-compose logs -f email-service

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Create database schema manually (service creates it automatically)
schema:
	mysql -h $(MYSQL_HOST) -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) $(MYSQL_DB) < schema.sql

# Verify database tables
verify-db:
	@echo "Verifying database tables..."
	@mysql -h $(MYSQL_HOST) -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) $(MYSQL_DB) -e "SHOW TABLES LIKE 'sent_reports_emails';"
	@mysql -h $(MYSQL_HOST) -u $(MYSQL_USER) -p$(MYSQL_PASSWORD) $(MYSQL_DB) -e "SHOW INDEX FROM sent_reports_emails;" 