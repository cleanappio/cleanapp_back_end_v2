#!/bin/bash

# Build script for report-tags service
# Usage: ./build_image.sh [version] [registry]

set -e

# Default values
VERSION=${1:-latest}
REGISTRY=${2:-""}
IMAGE_NAME="report-tags"

echo "Building report-tags service..."
echo "Version: $VERSION"
echo "Registry: $REGISTRY"

# Build the Docker image
docker build -t $IMAGE_NAME:$VERSION .

# Tag with registry if provided
if [ -n "$REGISTRY" ]; then
    FULL_IMAGE_NAME="$REGISTRY/$IMAGE_NAME:$VERSION"
    docker tag $IMAGE_NAME:$VERSION $FULL_IMAGE_NAME
    echo "Tagged as: $FULL_IMAGE_NAME"
    
    # Ask if user wants to push
    read -p "Push to registry? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Pushing $FULL_IMAGE_NAME..."
        docker push $FULL_IMAGE_NAME
        echo "Pushed successfully!"
    fi
else
    echo "Built image: $IMAGE_NAME:$VERSION"
fi

echo "Build complete!"
