.PHONY: help install setup-env run test test-imports clean docker-build docker-run

help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

install: ## Install Python dependencies
	pip install -r requirements.txt

setup-env: ## Create .env file from example
	@if [ ! -f .env ]; then \
		cp env.example .env; \
		echo "Created .env file from env.example"; \
		echo "Please edit .env with your configuration"; \
	else \
		echo ".env file already exists"; \
	fi

run: ## Run the Flask application
	@if [ -f .env ]; then \
		echo "Loading environment from .env file..."; \
		set -a && source .env && set +a && source venv/bin/activate && python app.py; \
	else \
		echo "No .env file found. Please run 'make setup-env' first."; \
		exit 1; \
	fi

run-venv: ## Run the Flask application with virtual environment and .env loaded
	@if [ -f .env ]; then \
		echo "Loading environment from .env file..."; \
		set -a && source .env && set +a && source venv/bin/activate && python app.py; \
	else \
		echo "No .env file found. Please run 'make setup-env' first."; \
		exit 1; \
	fi

run-simple: ## Run the Flask application with virtual environment only (no .env)
	source venv/bin/activate && python app.py

test: ## Run tests (placeholder)
	@echo "Tests not implemented yet"

test-service: ## Test the face-detector service with an image
	@echo "Usage: make test-service IMAGE=path/to/image.jpg"
	@if [ -z "$(IMAGE)" ]; then \
		echo "❌ Please specify an image file: make test-service IMAGE=path/to/image.jpg"; \
		exit 1; \
	fi
	@if [ ! -f "$(IMAGE)" ]; then \
		echo "❌ Image file not found: $(IMAGE)"; \
		exit 1; \
	fi
	@echo "🧪 Testing service with image: $(IMAGE)"
	@source venv/bin/activate && python test_service.py "$(IMAGE)"

test-imports: ## Test that all imports work correctly
	python -c "from config import Config; from app import app; print('All imports successful!')"

clean: ## Clean up Python cache files
	find . -type f -name "*.pyc" -delete
	find . -type d -name "__pycache__" -delete
	find . -type d -name "*.egg-info" -delete

docker-build: ## Build Docker image
	docker build -t face-detector .

docker-run: ## Run Docker container
	docker run -p 5000:5000 face-detector

docker-compose-up: ## Start services with docker-compose
	docker-compose up --build

docker-compose-down: ## Stop services with docker-compose
	docker-compose down
