#!/bin/bash

# Test script for report-tags service
# Make sure the service is running on localhost:8083

BASE_URL="http://localhost:8083"

echo "Testing Report Tags Service API"
echo "================================"

# Health check
echo "1. Health Check"
curl -s "$BASE_URL/health" | jq .
echo ""

# Add tags to a report
echo "2. Add tags to report 123"
curl -s -X POST "$BASE_URL/api/v3/reports/123/tags" \
  -H "Content-Type: application/json" \
  -d '{"tags": ["Beach", "cleanup", "Plastic"]}' | jq .
echo ""

# Get tags for a report
echo "3. Get tags for report 123"
curl -s "$BASE_URL/api/v3/reports/123/tags" | jq .
echo ""

# Get tag suggestions
echo "4. Get tag suggestions for 'beac'"
curl -s "$BASE_URL/api/v3/tags/suggest?q=beac&limit=5" | jq .
echo ""

# Get trending tags
echo "5. Get trending tags"
curl -s "$BASE_URL/api/v3/tags/trending?limit=5" | jq .
echo ""

# Follow a tag
echo "6. Follow tag 'beach' for user 'user123'"
curl -s -X POST "$BASE_URL/api/v3/users/user123/tags/follow" \
  -H "Content-Type: application/json" \
  -d '{"tag": "beach"}' | jq .
echo ""

# Get user follows
echo "7. Get follows for user 'user123'"
curl -s "$BASE_URL/api/v3/users/user123/tags/follows" | jq .
echo ""

# Get location feed
echo "8. Get location feed for user 'user123'"
curl -s "$BASE_URL/api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=user123&limit=5" | jq .
echo ""

echo "API tests completed!"
