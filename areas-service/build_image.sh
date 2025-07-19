#!/bin/bash

# Build script for areas-service

set -e

echo "Building areas-service..."

# Build the Docker image
docker build -t areas-service:latest .

echo "Areas service built successfully!"
echo "To run the service:"
echo "  docker run -p 8081:8081 areas-service:latest"
echo "Or with docker-compose:"
echo "  docker-compose up" 