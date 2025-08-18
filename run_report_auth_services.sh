#!/bin/bash

echo "Starting CleanApp services with report-auth microservice..."

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "ERROR: Docker is not running. Please start Docker first."
    exit 1
fi

# Build and start services
echo "Building and starting services..."
docker-compose -f docker-compose.report-auth.yml up --build -d

echo ""
echo "Services started successfully!"
echo ""
echo "Service URLs:"
echo "  Auth Service: http://localhost:8080"
echo "  Report Auth Service: http://localhost:8081"
echo "  Database: localhost:3306"
echo ""
echo "To view logs:"
echo "  docker-compose -f docker-compose.report-auth.yml logs -f"
echo ""
echo "To stop services:"
echo "  docker-compose -f docker-compose.report-auth.yml down"
