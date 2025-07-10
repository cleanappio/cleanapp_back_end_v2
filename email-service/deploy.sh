#!/bin/bash

# Email Service Deployment Script

set -e

echo "🚀 Deploying Email Service..."

# Check if SENDGRID_API_KEY is set
if [ -z "$SENDGRID_API_KEY" ]; then
    echo "❌ Error: SENDGRID_API_KEY environment variable is required"
    echo "Please set it with: export SENDGRID_API_KEY=your_api_key_here"
    exit 1
fi

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Error: Docker is not running"
    exit 1
fi

# Build the Docker image
echo "📦 Building Docker image..."
docker build -t email-service .

# Stop existing containers
echo "🛑 Stopping existing containers..."
docker-compose down || true

# Start the service
echo "▶️  Starting email service..."
docker-compose up -d

# Wait for service to be ready
echo "⏳ Waiting for service to be ready..."
sleep 10

# Check if service is running
if docker-compose ps | grep -q "Up"; then
    echo "✅ Email service deployed successfully!"
    echo "📊 View logs with: docker-compose logs -f email-service"
    echo "🛑 Stop service with: docker-compose down"
else
    echo "❌ Service failed to start"
    docker-compose logs email-service
    exit 1
fi 