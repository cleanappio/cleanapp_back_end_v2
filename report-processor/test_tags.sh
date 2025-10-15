#!/bin/bash

# Test script for tag functionality in report-processor
# Make sure the service is running on localhost:8080

BASE_URL="http://localhost:8080"

echo "Testing Report Processor Tag Functionality"
echo "=========================================="

# Health check
echo "1. Health Check"
curl -s "$BASE_URL/health" | jq .
echo ""

# Submit a report with tags
echo "2. Submit report with tags"
curl -s -X POST "$BASE_URL/api/v3/match_report" \
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

# Add tags to an existing report (assuming report_seq 1 exists)
echo "3. Add tags to existing report"
curl -s -X POST "$BASE_URL/api/v3/reports/tags" \
  -H "Content-Type: application/json" \
  -d '{
    "report_seq": 1,
    "tags": ["Ocean", "Pollution", "Environmental"]
  }' | jq .
echo ""

# Get tags for a report
echo "4. Get tags for report"
curl -s "$BASE_URL/api/v3/reports/tags?seq=1" | jq .
echo ""

echo "Tag functionality tests completed!"
