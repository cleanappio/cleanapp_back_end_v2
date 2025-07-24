# Report Processor API Usage Examples

## Prerequisites

1. The service is running on port 8081
2. The auth-service is running and accessible
3. You have a valid JWT token from the auth-service
4. The database is accessible and contains the `reports` table

## Examples

### 1. Mark a Report as Resolved

```bash
curl -X POST http://localhost:8081/api/v3/reports/mark_resolved \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "seq": 123
  }'
```

**Expected Response:**
```json
{
  "success": true,
  "message": "Report marked as resolved successfully",
  "seq": 123,
  "status": "resolved"
}
```

### 2. Get Report Status

```bash
curl -X GET "http://localhost:8081/api/v3/reports/status?seq=123"
```

**Expected Response:**
```json
{
  "success": true,
  "data": {
    "seq": 123,
    "status": "resolved",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
}
```

### 3. Get Report Status Counts

```bash
curl -X GET http://localhost:8081/api/v3/reports/status/count
```

**Expected Response:**
```json
{
  "success": true,
  "data": {
    "active": 10,
    "resolved": 5
  }
}
```

### 4. Health Check

```bash
curl -X GET http://localhost:8081/api/v3/reports/health
```

**Expected Response:**
```json
{
  "status": "healthy",
  "service": "report-processor",
  "timestamp": "2024-01-01T00:00:00Z"
}
```

## Error Examples

### Invalid Report Sequence

```bash
curl -X POST http://localhost:8081/api/v3/reports/mark_resolved \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "seq": 999999
  }'
```

**Expected Response:**
```json
{
  "success": false,
  "message": "Failed to mark report as resolved",
  "error": "report with seq 999999 does not exist"
}
```

### Missing Authentication

```bash
curl -X POST http://localhost:8081/api/v3/reports/mark_resolved \
  -H "Content-Type: application/json" \
  -d '{
    "seq": 123
  }'
```

**Expected Response:**
```json
{
  "error": "missing authorization header"
}
```

### Invalid Token

```bash
curl -X POST http://localhost:8081/api/v3/reports/mark_resolved \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer INVALID_TOKEN" \
  -d '{
    "seq": 123
  }'
```

**Expected Response:**
```json
{
  "error": "invalid or expired token"
}
```

### Auth Service Unavailable

If the auth-service is not running or not accessible, you'll get:

**Expected Response:**
```json
{
  "error": "invalid or expired token"
}
```

### Invalid Request Body

```bash
curl -X POST http://localhost:8081/api/v3/reports/mark_resolved \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "invalid_field": "value"
  }'
```

**Expected Response:**
```json
{
  "success": false,
  "message": "Invalid request body",
  "error": "Key: 'MarkResolvedRequest.Seq' Error:Field validation for 'Seq' failed on the 'required' tag"
}
```

## Using with JavaScript/Fetch

```javascript
// Mark a report as resolved
async function markReportResolved(seq, token) {
  const response = await fetch('http://localhost:8081/api/v3/reports/mark_resolved', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`
    },
    body: JSON.stringify({ seq })
  });
  
  return await response.json();
}

// Get report status
async function getReportStatus(seq) {
  const response = await fetch(`http://localhost:8081/api/v3/reports/status?seq=${seq}`);
  return await response.json();
}

// Get status counts
async function getStatusCounts() {
  const response = await fetch('http://localhost:8081/api/v3/reports/status/count');
  return await response.json();
}

// Usage
const token = 'your-jwt-token-from-auth-service';
markReportResolved(123, token).then(result => {
  console.log('Report marked as resolved:', result);
});

getReportStatus(123).then(result => {
  console.log('Report status:', result);
});

getStatusCounts().then(result => {
  console.log('Status counts:', result);
});
```

## Using with Python/Requests

```python
import requests
import json

# Mark a report as resolved
def mark_report_resolved(seq, token):
    url = 'http://localhost:8081/api/v3/reports/mark_resolved'
    headers = {
        'Content-Type': 'application/json',
        'Authorization': f'Bearer {token}'
    }
    data = {'seq': seq}
    
    response = requests.post(url, headers=headers, json=data)
    return response.json()

# Get report status
def get_report_status(seq):
    url = f'http://localhost:8081/api/v3/reports/status?seq={seq}'
    response = requests.get(url)
    return response.json()

# Get status counts
def get_status_counts():
    url = 'http://localhost:8081/api/v3/reports/status/count'
    response = requests.get(url)
    return response.json()

# Usage
token = 'your-jwt-token-from-auth-service'
result = mark_report_resolved(123, token)
print('Report marked as resolved:', result)

status = get_report_status(123)
print('Report status:', status)

counts = get_status_counts()
print('Status counts:', counts)
```

## Getting a Token from Auth Service

To get a token for testing, you can use the auth-service:

```bash
# Login to get a token
curl -X POST http://localhost:8080/api/v3/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123"
  }'
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "refresh_token_here",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

Use the `token` value in the Authorization header for subsequent requests to the report-processor service. 