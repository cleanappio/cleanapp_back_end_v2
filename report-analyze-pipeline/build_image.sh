#!/bin/bash

# Build script for report-analyze-pipeline service
# Usage: ./build_image.sh [dev|prod]

set -e

# Default to dev if no environment specified
ENVIRONMENT=${1:-dev}

# Load version from .version file
if [ -f .version ]; then
    BUILD_VERSION=$(cat .version | grep BUILD_VERSION | cut -d'=' -f2)
else
    BUILD_VERSION="1.0.0"
fi

echo "Building report-analyze-pipeline for environment: $ENVIRONMENT"
echo "Build version: $BUILD_VERSION"

# Set Docker registry and project
DOCKER_LOCATION="us-central1-docker.pkg.dev"
DOCKER_PROJECT="cleanup-mysql-v2"
DOCKER_REPO="cleanapp-docker-repo"
IMAGE_NAME="cleanapp-report-analyze-pipeline-image"

# Full image path
FULL_IMAGE_PATH="${DOCKER_LOCATION}/${DOCKER_PROJECT}/${DOCKER_REPO}/${IMAGE_NAME}:${ENVIRONMENT}"

echo "Building image: $FULL_IMAGE_PATH"

# Build the Docker image
docker build -t "$FULL_IMAGE_PATH" .

echo "Image built successfully: $FULL_IMAGE_PATH"

# Tag with version
VERSIONED_IMAGE_PATH="${DOCKER_LOCATION}/${DOCKER_PROJECT}/${DOCKER_REPO}/${IMAGE_NAME}:${BUILD_VERSION}"
docker tag "$FULL_IMAGE_PATH" "$VERSIONED_IMAGE_PATH"

echo "Versioned image: $VERSIONED_IMAGE_PATH"

# Push to registry
echo "Pushing images to registry..."
docker push "$FULL_IMAGE_PATH"
docker push "$VERSIONED_IMAGE_PATH"

echo "Build and push completed successfully!"
echo "Environment image: $FULL_IMAGE_PATH"
echo "Versioned image: $VERSIONED_IMAGE_PATH" 