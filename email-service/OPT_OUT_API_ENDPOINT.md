# Opt-Out API Endpoint

## Overview
The Email Service now includes an HTTP API endpoint that allows users to opt out of receiving emails. This endpoint provides a RESTful interface for managing email opt-outs while maintaining the existing polling functionality for report processing.

## API Endpoint

### Opt-Out Email
**Endpoint**: `POST /api/v3/optout`  
**Content-Type**: `application/json`

#### Request Body
```json
{
  "email": "user@example.com"
}
```

#### Response
**Success (200 OK)**:
```json
{
  "success": true,
  "message": "Email user@example.com has been opted out successfully"
}
```

**Error (400 Bad Request)**:
```json
{
  "error": "Email is required"
}
```

**Error (500 Internal Server Error)**:
```json
{
  "error": "Failed to opt out email: email user@example.com is already opted out"
}
```

### Opt-Out Link (Email Integration)
**Endpoint**: `GET /opt-out?email=user@example.com`  
**Content-Type**: `text/html`

#### Query Parameters
- `email`: Email address to opt out (required)

#### Response
**Success (200 OK)**: HTML page confirming successful opt-out
**Error (400 Bad Request)**: HTML page showing error message

### Health Check
**Endpoint**: `GET /health`  
**Response**: Service status and timestamp

## Implementation Details

### 1. HTTP Server Integration
- **File**: `main.go`
- **Changes**:
  - Added HTTP server alongside existing polling service
  - Uses **Gin framework** for high-performance HTTP routing
  - Graceful shutdown handling
  - Configurable HTTP port (default: 8080)

### 2. Request Handler
- **File**: `handlers/handlers.go`
- **Function**: `HandleOptOut`
- **Features**:
  - **Gin context-based handling** for better performance
  - **Automatic JSON binding and validation** using Gin's binding
  - **Structured error responses** with proper HTTP status codes
  - **Built-in request validation** with `binding:"required"` tags

### 3. Service Layer
- **File**: `service/email_service.go`
- **Function**: `AddOptedOutEmail`
- **Features**:
  - Database interaction for opted-out emails
  - Duplicate email checking
  - Comprehensive error handling
  - Logging for audit trail

## Database Schema

### opted_out_emails Table
```sql
CREATE TABLE opted_out_emails (
    id INT AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    opted_out_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_email (email),
    INDEX idx_opted_out_at (opted_out_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
```

**Fields**:
- `id`: Unique identifier (auto-increment)
- `email`: Email address (unique, indexed)
- `opted_out_at`: Timestamp when email was opted out

**Indexes**:
- Primary key on `id`
- Unique index on `email` for fast lookups
- Index on `opted_out_at` for audit queries

## Usage Examples

### Environment Variable Configuration
```bash
# Set custom poll interval
export POLL_INTERVAL=60s

# Set custom HTTP port
export HTTP_PORT=9090

# Set custom opt-out URL
export OPT_OUT_URL="https://yourdomain.com/opt-out"

# Set database configuration
export DB_HOST=your-db-host
export DB_PORT=3306
export DB_NAME=cleanapp
export DB_USER=your-db-user
export DB_PASSWORD=your-db-password

# Set SendGrid configuration
export SENDGRID_API_KEY=your-api-key
export SENDGRID_FROM_NAME="CleanApp"
export SENDGRID_FROM_EMAIL="info@cleanapp.io"
```

### Running the Service
```bash
# Start with default settings
./main

# Start with custom configuration via environment variables
export POLL_INTERVAL=60s
export HTTP_PORT=9090
./main
```

### cURL Commands

#### Opt-out an email:
```bash
curl -X POST http://localhost:8080/api/v3/optout \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com"}'
```

#### Check service health:
```bash
curl http://localhost:8080/health
```

### JavaScript/Fetch API
```javascript
const optOutEmail = async (email) => {
  try {
    const response = await fetch('http://localhost:8080/api/v3/optout', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ email })
    });
    
    const result = await response.json();
    
    if (response.ok) {
      console.log('Success:', result.message);
    } else {
      console.error('Error:', result.error);
    }
  } catch (error) {
    console.error('Network error:', error);
  }
};

// Usage
optOutEmail('user@example.com');
```

### Python Requests
```python
import requests
import json

def opt_out_email(email):
    url = "http://localhost:8080/api/v3/optout"
    payload = {"email": email}
    headers = {"Content-Type": "application/json"}
    
    try:
        response = requests.post(url, json=payload, headers=headers)
        result = response.json()
        
        if response.status_code == 200:
            print(f"Success: {result['message']}")
        else:
            print(f"Error: {result.get('error', 'Unknown error')}")
            
    except requests.exceptions.RequestException as e:
        print(f"Network error: {e}")

# Usage
opt_out_email("user@example.com")
```

## Error Handling

### Validation Errors
- **Empty email**: Returns 400 Bad Request
- **Missing email field**: Returns 400 Bad Request
- **Invalid JSON**: Returns 400 Bad Request

### Business Logic Errors
- **Email already opted out**: Returns 500 Internal Server Error
- **Database connection issues**: Returns 500 Internal Server Error

### Network Errors
- **Service unavailable**: Connection refused/timeout
- **Invalid endpoint**: 404 Not Found

## Configuration

### Environment Variables
- **POLL_INTERVAL**: How often to poll for reports (default: 10s)
- **HTTP_PORT**: HTTP server port (default: 8080)
- **OPT_OUT_URL**: URL for email opt-out links (default: http://localhost:8080/opt-out)

### Command Line Configuration
The service no longer uses command line flags. All configuration is handled through environment variables for better containerization and deployment.

## Security Considerations

### Input Validation
- Email format validation (basic)
- SQL injection prevention (parameterized queries)
- Request size limits (built-in HTTP limits)

### Access Control
- **No authentication**: Endpoint is publicly accessible
- **Rate limiting**: Not implemented (consider for production)
- **IP restrictions**: Not implemented (consider for production)

### Data Protection
- **Email storage**: Emails stored in database
- **Audit trail**: Timestamps for all opt-outs
- **No PII exposure**: Only success/error messages returned

## Testing

### Test Script
A comprehensive test script is provided: `test_optout_api.sh`

**Test Cases**:
1. **Valid opt-out request**: Should succeed with 200 OK
2. **Empty email**: Should fail with 400 Bad Request
3. **Missing email field**: Should fail with 400 Bad Request
4. **Health check**: Should return service status

### Manual Testing
```bash
# Start the service
./main

# In another terminal, run tests
./test_optout_api.sh
```

### Integration Testing
- Verify database entries are created
- Check email filtering works correctly
- Test duplicate opt-out prevention
- Validate error handling scenarios

## Monitoring and Logging

### Application Logs
- **Opt-out success**: `Email user@example.com has been opted out successfully`
- **Duplicate attempts**: `email user@example.com is already opted out`
- **Database errors**: Detailed error messages with context

### Health Monitoring
- **Endpoint**: `/health`
- **Response time**: Included in health check
- **Service status**: Always returns "healthy" when running

### Database Monitoring
- **Table size**: Monitor `opted_out_emails` table growth
- **Index performance**: Monitor query performance on email lookups
- **Storage usage**: Track database storage consumption

## Performance Considerations

### Database Performance
- **Indexed queries**: Fast email lookups with `idx_email`
- **Unique constraints**: Prevents duplicate entries
- **Minimal data**: Only essential fields stored

### HTTP Performance
- **Lightweight responses**: JSON responses are minimal
- **Connection pooling**: Database connections are reused
- **Async processing**: Email processing doesn't block HTTP requests

### Scalability
- **Stateless design**: No session state maintained
- **Database scaling**: Can scale database independently
- **Load balancing**: Multiple instances can share database

## Deployment

### Docker
```bash
# Build image
docker build -t email-service .

# Run container
docker run -p 8080:8080 \
  -e DB_HOST=your-db-host \
  -e DB_PORT=3306 \
  -e DB_NAME=your-db-name \
  -e DB_USER=your-db-user \
  -e DB_PASSWORD=your-db-password \
  email-service
```

### Environment Variables
```bash
export DB_HOST=localhost
export DB_PORT=3306
export DB_NAME=cleanapp
export DB_USER=root
export DB_PASSWORD=your-password
export SENDGRID_API_KEY=your-api-key
```

### Health Checks
```bash
# Kubernetes health check
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Future Enhancements

### Planned Features
1. **Bulk opt-out**: Support for multiple emails
2. **Opt-in functionality**: Allow users to opt back in
3. **Opt-out reasons**: Track why users opted out
4. **Analytics**: Opt-out rate monitoring
5. **Webhook support**: Notify external systems of opt-outs

### Security Improvements
1. **Authentication**: JWT or API key authentication
2. **Rate limiting**: Prevent abuse
3. **IP restrictions**: Limit access to specific networks
4. **Audit logging**: Comprehensive activity tracking

### Performance Optimizations
1. **Caching**: Redis cache for frequently checked emails
2. **Batch processing**: Bulk database operations
3. **Connection pooling**: Optimize database connections
4. **Response compression**: Gzip HTTP responses

## Troubleshooting

### Common Issues

#### Service won't start
- Check database connectivity
- Verify environment variables
- Check port availability

#### API requests fail
- Verify service is running
- Check endpoint URL
- Validate JSON format

#### Database errors
- Check database permissions
- Verify table structure
- Monitor database logs

### Debug Mode
```bash
# Run with verbose logging
./main -v

# Check database tables
mysql -u root -p cleanapp -e "SHOW TABLES;"

# Verify opted_out_emails table
mysql -u root -p cleanapp -e "DESCRIBE opted_out_emails;"
```

## Summary

The opt-out API endpoint provides:
- **RESTful interface** for email opt-out management
- **Robust error handling** with appropriate HTTP status codes
- **Database persistence** with proper indexing
- **Comprehensive logging** for monitoring and debugging
- **Easy integration** with existing systems
- **Scalable architecture** for production use

This enhancement makes the email service more user-friendly and compliant with email regulations while maintaining the existing functionality for report processing and email delivery.

## Dependencies

### Required Packages
- **Gin Framework**: `github.com/gin-gonic/gin` - High-performance HTTP web framework
- **MySQL Driver**: `github.com/go-sql-driver/mysql` - Database connectivity
- **SendGrid**: `github.com/sendgrid/sendgrid-go` - Email delivery service
- **Logging**: `github.com/apex/log` - Structured logging

### Framework Benefits
- **Performance**: Gin is one of the fastest HTTP frameworks for Go
- **Validation**: Built-in request binding and validation
- **Middleware**: Extensive middleware support for logging, CORS, etc.
- **Error Handling**: Comprehensive error handling and response formatting

## Email Template Integration

### Opt-Out Links in Emails
The email service now automatically includes opt-out links in all email templates:

#### HTML Emails
- **Opt-out link**: Clickable link with recipient's email pre-filled
- **Visual styling**: Styled footer section with clear instructions
- **User-friendly**: Easy one-click opt-out process

#### Plain Text Emails
- **Opt-out URL**: Direct link with email parameter
- **Clear instructions**: Simple text-based opt-out process
- **Alternative methods**: Reply with "UNSUBSCRIBE" option

### Email Template Features
- **Automatic integration**: Opt-out links added to all report emails
- **Personalized links**: Each email contains recipient-specific opt-out URL
- **Professional appearance**: Clean, branded opt-out footer
- **Compliance ready**: Meets email unsubscribe requirements

### Configuration
The opt-out URL is configurable via environment variable:
```bash
export OPT_OUT_URL="https://yourdomain.com/opt-out"
```

**Default value**: `http://localhost:8080/opt-out`
