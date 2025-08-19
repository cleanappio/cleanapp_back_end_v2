#!/bin/bash

# Test script for report-ownership-service

SERVICE_URL="http://localhost:8082"

echo "Testing Report Ownership Service"
echo "================================"
echo ""

echo "1. Testing health endpoint:"
curl -s "$SERVICE_URL/health" | jq .
echo ""

echo "2. Testing status endpoint:"
curl -s "$SERVICE_URL/status" | jq .
echo ""

echo "3. Checking if service is running:"
if curl -s "$SERVICE_URL/health" > /dev/null; then
    echo "✅ Service is running and responding"
else
    echo "❌ Service is not responding"
fi
echo ""

echo "4. Service information:"
echo "   - Health endpoint: $SERVICE_URL/health"
echo "   - Status endpoint: $SERVICE_URL/status"
echo "   - Port: 8082"
echo ""

echo "5. To view service logs:"
echo "   docker-compose logs -f report-ownership-service"
echo ""

echo "6. To stop the service:"
echo "   docker-compose down"
echo ""

echo "=========================================="
echo "Service Status Summary:"
echo "- The service polls for new reports every 30 seconds (configurable)"
echo "- It processes reports in batches of 100 (configurable)"
echo "- Ownership is determined by location and brand analysis"
echo "- Results are stored in the reports_owners table"
echo "- One report can have multiple owners"
echo "- The service runs continuously in the background"
