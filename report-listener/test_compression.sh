#!/bin/bash

# Test script to verify gzip compression is working on /reports/last endpoint
# This script tests the compression by checking the Content-Encoding header

set -e

SERVICE_URL="http://localhost:8080"
ENDPOINT="/api/v3/reports/last"

echo "Testing gzip compression on $ENDPOINT endpoint..."

# Test with a small number of reports first
echo "1. Testing with n=5 (small response)..."
response=$(curl -s -H "Accept-Encoding: gzip" -H "Accept: application/json" \
    -w "HTTP_CODE:%{http_code}\nCONTENT_ENCODING:%{content_encoding}\nSIZE_DOWNLOADED:%{size_download}\n" \
    "$SERVICE_URL$ENDPOINT?n=5")

echo "Response details:"
echo "$response"

# Check if Content-Encoding is gzip
if echo "$response" | grep -q "CONTENT_ENCODING:gzip"; then
    echo "✅ Compression is working! Content-Encoding: gzip"
else
    echo "❌ Compression is not working. Content-Encoding: $(echo "$response" | grep "CONTENT_ENCODING:")"
fi

echo ""
echo "2. Testing with n=50 (larger response)..."
response=$(curl -s -H "Accept-Encoding: gzip" -H "Accept: application/json" \
    -w "HTTP_CODE:%{http_code}\nCONTENT_ENCODING:%{content_encoding}\nSIZE_DOWNLOADED:%{size_download}\n" \
    "$SERVICE_URL$ENDPOINT?n=50")

echo "Response details:"
echo "$response"

# Check if Content-Encoding is gzip
if echo "$response" | grep -q "CONTENT_ENCODING:gzip"; then
    echo "✅ Compression is working! Content-Encoding: gzip"
else
    echo "❌ Compression is not working. Content-Encoding: $(echo "$response" | grep "CONTENT_ENCODING:")"
fi

echo ""
echo "3. Testing without Accept-Encoding header (should not compress)..."
response=$(curl -s -H "Accept: application/json" \
    -w "HTTP_CODE:%{http_code}\nCONTENT_ENCODING:%{content_encoding}\nSIZE_DOWNLOADED:%{size_download}\n" \
    "$SERVICE_URL$ENDPOINT?n=5")

echo "Response details:"
echo "$response"

# Check if Content-Encoding is empty (no compression)
if echo "$response" | grep -q "CONTENT_ENCODING:$"; then
    echo "✅ No compression when Accept-Encoding not specified (expected)"
else
    echo "❌ Unexpected compression when Accept-Encoding not specified"
fi

echo ""
echo "Compression test complete!"
