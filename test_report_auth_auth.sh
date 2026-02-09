#!/bin/bash

# Test script for report-auth service authentication
# This script demonstrates both authentication methods

echo "Testing Report Auth Service Authentication"
echo "=========================================="
echo ""

# Base URL for the report-auth service
REPORT_AUTH_URL="http://localhost:8081"

echo "1. Testing without authentication (should fail):"
echo "-----------------------------------------------"
curl -s -X POST "$REPORT_AUTH_URL/api/v3/reports/authorization" \
  -H "Content-Type: application/json" \
  -d '{"report_seqs": [1, 2, 3]}' | jq .
echo ""

echo "2. Testing with invalid JWT token (should fail):"
echo "------------------------------------------------"
curl -s -X POST "$REPORT_AUTH_URL/api/v3/reports/authorization" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $INVALID_JWT_TOKEN" \
  -d '{"report_seqs": [1, 2, 3]}' | jq .
echo ""

echo "3. Testing with internal service header (should work if service is running):"
echo "--------------------------------------------------------------------------"
curl -s -X POST "$REPORT_AUTH_URL/api/v3/reports/authorization" \
  -H "Content-Type: application/json" \
  -H "X-User-ID: test_user_123" \
  -d '{"report_seqs": [1, 2, 3]}' | jq .
echo ""

echo "4. Testing health endpoint (should work without auth):"
echo "-----------------------------------------------------"
curl -s "$REPORT_AUTH_URL/health" | jq .
echo ""

echo "5. Testing API health endpoint (should work without auth):"
echo "----------------------------------------------------------"
curl -s "$REPORT_AUTH_URL/api/v3/health" | jq .
echo ""

echo "Note: For JWT token authentication, you need to:"
echo "1. Login to the auth-service to get a valid token"
echo "2. Use that token in the Authorization header"
echo "3. The token will be validated by calling the auth-service"
echo ""
echo "Example with valid JWT:"
echo "curl -X POST \"$REPORT_AUTH_URL/api/v3/reports/authorization\" \\"
echo "  -H \"Content-Type: application/json\" \\"
echo "  -H \"Authorization: Bearer $JWT_TOKEN\" \\"
echo "  -d '{\"report_seqs\": [1, 2, 3]}'"
