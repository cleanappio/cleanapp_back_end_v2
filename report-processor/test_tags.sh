#!/bin/bash

# Test script for optimized tag functionality
# Make sure both services are running:
# - report-processor on localhost:8080
# - report-tags on localhost:8083

REPORT_PROCESSOR_URL="http://localhost:8080"
TAG_SERVICE_URL="http://localhost:8083"

echo "Testing Optimized Tag Architecture"
echo "=================================="
echo "Report Processor: $REPORT_PROCESSOR_URL"
echo "Tag Service: $TAG_SERVICE_URL"
echo ""

# Health checks
echo "1. Health Checks"
echo "Report Processor:"
curl -s "$REPORT_PROCESSOR_URL/health" | jq .
echo ""
echo "Tag Service:"
curl -s "$TAG_SERVICE_URL/health" | jq .
echo ""

# Submit a report with tags (calls tag service internally)
echo "2. Submit report with tags (via report-processor)"
curl -s -X POST "$REPORT_PROCESSOR_URL/api/v3/match_report" \
  -H "Content-Type: application/json" \
  -d '{
    "version": "2.0",
    "id": "test-report-123",
    "latitude": 40.7128,
    "longitude": -74.0060,
    "x": 0.5,
    "y": 0.5,
    "image": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
    "annotation": "Test litter report",
    "tags": ["Beach", "Plastic", "Cleanup"]
  }' | jq .
echo ""

# Test tag service directly
echo "3. Add tags to existing report (via tag service)"
curl -s -X POST "$TAG_SERVICE_URL/api/v3/reports/1/tags" \
  -H "Content-Type: application/json" \
  -d '{
    "tags": ["Ocean", "Pollution", "Environmental"]
  }' | jq .
echo ""

# Get tags for a report
echo "4. Get tags for report (via tag service)"
curl -s "$TAG_SERVICE_URL/api/v3/reports/1/tags" | jq .
echo ""

# Test tag suggestions
echo "5. Get tag suggestions"
curl -s "$TAG_SERVICE_URL/api/v3/tags/suggest?q=beac&limit=5" | jq .
echo ""

# Test trending tags
echo "6. Get trending tags"
curl -s "$TAG_SERVICE_URL/api/v3/tags/trending?limit=5" | jq .
echo ""

echo "Optimized tag architecture tests completed!"
echo ""
echo "Architecture Summary:"
echo "- report-processor: Handles report submission and calls tag service for tags"
echo "- report-tags: Full-featured tag microservice with all advanced features"
echo "- No duplicate functionality between services"
