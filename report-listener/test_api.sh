#!/bin/bash

# Test script for report-listener API
# Make sure the service is running on localhost:8080

BASE_URL="http://localhost:8080"

echo "Testing report-listener API endpoints..."
echo "========================================"

# Test health check
echo "1. Testing health check..."
curl -s "$BASE_URL/health" | jq '.'
echo ""

# Test GetLastNAnalyzedReports with full_data=true (default)
echo "2. Testing GetLastNAnalyzedReports with full_data=true (default)..."
curl -s "$BASE_URL/api/reports/last?n=5" | jq '.'
echo ""

# Test GetLastNAnalyzedReports with full_data=false
echo "3. Testing GetLastNAnalyzedReports with full_data=false..."
curl -s "$BASE_URL/api/reports/last?n=5&full_data=false&classification=physical" | jq '.'
echo ""

# Test GetLastNAnalyzedReports with digital classification
echo "4. Testing GetLastNAnalyzedReports with digital classification..."
curl -s "$BASE_URL/api/reports/last?n=5&full_data=false&classification=digital" | jq '.'
echo ""

# Test GetLastNAnalyzedReports with custom limit
echo "5. Testing GetLastNAnalyzedReports with custom limit..."
curl -s "$BASE_URL/api/reports/last?n=3&full_data=true&classification=physical" | jq '.'
echo ""

# Test error handling - invalid full_data parameter
echo "6. Testing error handling - invalid full_data parameter..."
curl -s "$BASE_URL/api/reports/last?n=5&full_data=invalid&classification=physical" | jq '.'
echo ""

# Test error handling - invalid n parameter
echo "7. Testing error handling - invalid n parameter..."
curl -s "$BASE_URL/api/reports/last?n=invalid&classification=physical" | jq '.'
echo ""

echo "Testing completed!" 