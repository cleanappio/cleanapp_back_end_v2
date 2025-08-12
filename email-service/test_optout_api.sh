#!/bin/bash

# Test script for the opt-out API endpoint and opt-out link
# Make sure the email-service is running first
# The service now uses Gin framework for better performance
# Configuration is now handled via environment variables

BASE_URL="http://localhost:8080"
API_ENDPOINT="/api/v3/optout"
OPT_OUT_LINK="/opt-out"

echo "Testing Email Service Opt-Out API and Link (Gin Framework)"
echo "=========================================================="
echo ""

# Test 1: Valid opt-out request via API
echo "Test 1: Valid opt-out request via API"
echo "POST $BASE_URL$API_ENDPOINT"
echo '{"email": "test@example.com"}'
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com"}' \
  "$BASE_URL$API_ENDPOINT")

echo "Response:"
echo "$response"
echo ""

# Test 2: Empty email via API (should fail)
echo "Test 2: Empty email via API (should fail)"
echo "POST $BASE_URL$API_ENDPOINT"
echo '{"email": ""}'
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"email": ""}' \
  "$BASE_URL$API_ENDPOINT")

echo "Response:"
echo "$response"
echo ""

# Test 3: Missing email field via API (should fail)
echo "Test 3: Missing email field via API (should fail)"
echo "POST $BASE_URL$API_ENDPOINT"
echo '{}'
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{}' \
  "$BASE_URL$API_ENDPOINT")

echo "Response:"
echo "$response"
echo ""

# Test 4: Invalid JSON via API (should fail)
echo "Test 4: Invalid JSON via API (should fail)"
echo "POST $BASE_URL$API_ENDPOINT"
echo '{"email": "test@example.com"'
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com"' \
  "$BASE_URL$API_ENDPOINT")

echo "Response:"
echo "$response"
echo ""

# Test 5: Opt-out link with email parameter (should succeed)
echo "Test 5: Opt-out link with email parameter (should succeed)"
echo "GET $BASE_URL$OPT_OUT_LINK?email=test2@example.com"
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X GET \
  "$BASE_URL$OPT_OUT_LINK?email=test2@example.com")

echo "Response (HTML):"
echo "$response" | head -20
echo "... (truncated for readability)"
echo ""

# Test 6: Opt-out link without email parameter (should fail)
echo "Test 6: Opt-out link without email parameter (should fail)"
echo "GET $BASE_URL$OPT_OUT_LINK"
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X GET \
  "$BASE_URL$OPT_OUT_LINK")

echo "Response (HTML):"
echo "$response" | head -20
echo "... (truncated for readability)"
echo ""

# Test 7: Health check
echo "Test 7: Health check"
echo "GET $BASE_URL/health"
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X GET \
  "$BASE_URL/health")

echo "Response:"
echo "$response"
echo ""

# Test 8: Test Gin's automatic method validation
echo "Test 8: Test Gin's automatic method validation"
echo "GET $BASE_URL$API_ENDPOINT (should fail - method not allowed)"
echo ""

response=$(curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X GET \
  "$BASE_URL$API_ENDPOINT")

echo "Response:"
echo "$response"
echo ""

echo "Testing complete!"
echo ""
echo "Note: This service now uses the Gin framework for:"
echo "- Better performance and lower memory usage"
echo "- Automatic request validation and binding"
echo "- Built-in middleware support"
echo "- Enhanced error handling"
echo "- HTML template support for opt-out pages"
echo ""
echo "New features added:"
echo "- Opt-out link in email templates"
echo "- Web-based opt-out pages with HTML templates"
echo "- Configurable opt-out URL via OPT_OUT_URL environment variable"
echo "- Environment-based configuration (no more command line flags)"
echo ""
echo "Configuration via environment variables:"
echo "- POLL_INTERVAL: How often to poll for reports (default: 10s)"
echo "- HTTP_PORT: HTTP server port (default: 8080)"
echo "- OPT_OUT_URL: URL for email opt-out links"
echo "- DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD: Database configuration"
echo "- SENDGRID_API_KEY: SendGrid API key for email sending"
