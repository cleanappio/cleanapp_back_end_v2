#!/bin/bash

# Build script for report-ownership-service

echo "Building report-ownership-service Docker image..."

# Build the Docker image
docker build -t report-ownership-service:latest .

if [ $? -eq 0 ]; then
    echo "✅ Docker image built successfully!"
    echo "Image: report-ownership-service:latest"
    echo ""
    echo "To run the service:"
    echo "  docker-compose up"
    echo ""
    echo "To run in background:"
    echo "  docker-compose up -d"
    echo ""
    echo "To stop the service:"
    echo "  docker-compose down"
else
    echo "❌ Failed to build Docker image"
    exit 1
fi
