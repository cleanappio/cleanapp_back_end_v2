#!/bin/bash

# Test script for the Report Authorization API
# Make sure the auth service is running on localhost:8080

# Test data
REPORT_SEQS="[123, 456, 789]"

echo "Testing Report Authorization API..."
echo "=================================="

# Test without authentication (should fail)
echo "1. Testing without authentication (should fail):"
curl -X POST http://localhost:8080/api/v3/reports/authorization \
  -H "Content-Type: application/json" \
  -d "{\"report_seqs\": $REPORT_SEQS}" \
  -w "\nHTTP Status: %{http_code}\n\n"

# Test with invalid JSON (should fail)
echo "2. Testing with invalid JSON (should fail):"
curl -X POST http://localhost:8080/api/v3/reports/authorization \
  -H "Content-Type: application/json" \
  -d "{\"report_seqs\": [123, \"invalid\"]}" \
  -w "\nHTTP Status: %{http_code}\n\n"

# Test with empty report list (should fail)
echo "3. Testing with empty report list (should fail):"
curl -X POST http://localhost:8080/api/v3/reports/authorization \
  -H "Content-Type: application/json" \
  -d "{\"report_seqs\": []}" \
  -w "\nHTTP Status: %{http_code}\n\n"

echo "=================================="
echo "Note: To test with authentication, you need to:"
echo "1. Get a valid JWT token by logging in"
echo "2. Use the token in the Authorization header"
echo "3. Example:"
echo "   curl -X POST http://localhost:8080/api/v3/reports/authorization \\"
echo "     -H \"Authorization: Bearer <your_jwt_token>\" \\"
echo "     -H \"Content-Type: application/json\" \\"
echo "     -d '{\"report_seqs\": [123, 456, 789]}'"
