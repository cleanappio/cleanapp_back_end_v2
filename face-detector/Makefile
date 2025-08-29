# Makefile for face-detector service

.PHONY: install run run-venv run-simple setup-env test-imports test-service clean docker-build docker-run

# Install dependencies
install:
	pip install -r requirements.txt

# Run with virtual environment activation and .env loading
run:
	set -a && source .env && set +a && source venv/bin/activate && uvicorn app:app --host $(HOST) --port $(PORT)

# Run with virtual environment activation
run-venv:
	source venv/bin/activate && uvicorn app:app --host $(HOST) --port $(PORT)

# Run without virtual environment (for testing)
run-simple:
	uvicorn app:app --host $(HOST) --port $(PORT)

# Run with development mode (auto-reload)
run-dev:
	set -a && source .env && set +a && source venv/bin/activate && uvicorn app:app --host $(HOST) --port $(PORT) --reload

# Run with production settings
run-prod:
	set -a && source .env && set +a && source venv/bin/activate && uvicorn app:app --host $(HOST) --port $(PORT) --workers $(WORKERS) --no-access-log

# Setup virtual environment
setup-env:
	python -m venv venv
	source venv/bin/activate && pip install --upgrade pip
	source venv/bin/activate && pip install -r requirements.txt

# Test imports
test-imports:
	source venv/bin/activate && python -c "from app import app; print('All imports successful!')"

# Test service
test-service:
	source venv/bin/activate && python test_service.py

# Clean up
clean:
	rm -rf venv
	find . -type f -name "*.pyc" -delete
	find . -type d -name "__pycache__" -delete

# Docker commands
docker-build:
	docker build -t face-detector .

docker-build-clean:
	docker build --no-cache -t face-detector .

docker-run:
	docker run -p $(PORT):$(PORT) --env-file .env face-detector

docker-compose-up:
	docker-compose up --build

docker-compose-down:
	docker-compose down

# Default values
HOST ?= 0.0.0.0
PORT ?= 8080
WORKERS ?= 4
