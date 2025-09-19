#!/bin/bash

# Test script for report-analysis-backfill service
# This script tests the service endpoints

set -e

SERVICE_URL="http://localhost:8081"

echo "Testing report-analysis-backfill service..."

# Test health endpoint
echo "1. Testing health endpoint..."
curl -s "$SERVICE_URL/api/v1/health" | jq '.' || echo "Health check failed"

echo -e "\n2. Testing status endpoint..."
curl -s "$SERVICE_URL/api/v1/status" | jq '.' || echo "Status check failed"

echo -e "\n3. Service test complete!"
