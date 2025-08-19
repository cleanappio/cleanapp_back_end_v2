#!/bin/bash

# Test script for report-auth-service public access functionality
# This script demonstrates both authenticated and non-authenticated requests

REPORT_AUTH_URL="http://localhost:8081"

echo "Testing Report Auth Service Public Access Functionality"
echo "======================================================"
echo ""

echo "1. Testing without authentication (public access):"
echo "   This should only authorize reports that don't belong to any customer"
curl -s -X POST "$REPORT_AUTH_URL/api/v3/reports/authorization" \
  -H "Content-Type: application/json" \
  -d '{"report_seqs": [1, 2, 3, 4, 5]}' | jq .
echo ""

echo "2. Testing with internal service header (authenticated access):"
echo "   This should authorize reports based on user ownership"
curl -s -X POST "$REPORT_AUTH_URL/api/v3/reports/authorization" \
  -H "Content-Type: application/json" \
  -H "X-User-ID: test_user_123" \
  -d '{"report_seqs": [1, 2, 3, 4, 5]}' | jq .
echo ""

echo "3. Testing with JWT token (authenticated access):"
echo "   This should authorize reports based on user ownership"
echo "   Note: Replace 'your_jwt_token_here' with a valid token from auth-service"
curl -s -X POST "$REPORT_AUTH_URL/api/v3/reports/authorization" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your_jwt_token_here" \
  -d '{"report_seqs": [1, 2, 3, 4, 5]}' | jq .
echo ""

echo "4. Testing health endpoint (should always work):"
curl -s "$REPORT_AUTH_URL/health" | jq .
echo ""

echo "======================================================"
echo "Summary:"
echo "- Requests without authentication will only authorize public reports"
echo "- Requests with authentication will authorize based on user ownership"
echo "- Public reports are those not restricted to any specific customer"
echo "- The service provides clear reasons for authorization decisions"
