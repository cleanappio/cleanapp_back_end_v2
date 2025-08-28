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
	python app.py

test: ## Run tests (placeholder)
	@echo "Tests not implemented yet"

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
