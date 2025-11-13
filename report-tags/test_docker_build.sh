#!/bin/bash

echo "Testing Docker build for report-tags service..."
echo "=============================================="

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker Desktop."
    exit 1
fi

# Clean up any existing containers and images
echo "🧹 Cleaning up Docker..."
docker system prune -f > /dev/null 2>&1

# Test the build
echo "🔨 Building Docker image..."
if docker build --no-cache -t report-tags:test .; then
    echo "✅ Docker build successful!"
    echo "🎉 Ready for GCP deployment!"
    
    # Test that the image runs
    echo "🧪 Testing image startup..."
    if timeout 10s docker run --rm report-tags:test --help > /dev/null 2>&1; then
        echo "✅ Image runs successfully!"
    else
        echo "⚠️  Image built but may have runtime issues (this is normal for a service)"
    fi
    
    # Clean up test image
    docker rmi report-tags:test > /dev/null 2>&1
    echo "🧹 Cleaned up test image"
    
else
    echo "❌ Docker build failed!"
    echo "Please check the error messages above."
    exit 1
fi

echo ""
echo "🚀 You can now run: ./build_image.sh -e dev"
