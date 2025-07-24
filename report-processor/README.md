# Report Processor Microservice

A microservice for managing report statuses in the CleanApp system.

## Features

- Mark reports as resolved
- Get report status information
- Get report status counts
- Bearer token authentication via auth-service
- CORS support
- Health check endpoints

## Database Schema

The service creates and manages a `report_status` table with the following structure:

```sql
CREATE TABLE report_status (
    seq INT NOT NULL,
    status ENUM('active', 'resolved') NOT NULL DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (seq),
    FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE
);
```

## API Endpoints

### Protected Endpoints (Require Bearer Token)

#### POST /api/v3/reports/mark_resolved
Marks a report as resolved.

**Request Body:**
```json
{
    "seq": 123
}
```

**Response:**
```json
{
    "success": true,
    "message": "Report marked as resolved successfully",
    "seq": 123,
    "status": "resolved"
}
```

### Public Endpoints

#### GET /api/v3/reports/health
Health check endpoint.

**Response:**
```json
{
    "status": "healthy",
    "service": "report-processor",
    "timestamp": "2024-01-01T00:00:00Z"
}
```

#### GET /api/v3/reports/status?seq=123
Get the status of a specific report.

**Response:**
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

#### GET /api/v3/reports/status/count
Get the count of reports by status.

**Response:**
```json
{
    "success": true,
    "data": {
        "active": 10,
        "resolved": 5
    }
}
```

#### GET /health
Root health check endpoint.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | Database host |
| `DB_PORT` | `3306` | Database port |
| `DB_USER` | `server` | Database user |
| `DB_PASSWORD` | `secret_app` | Database password |
| `DB_NAME` | `cleanapp` | Database name |
| `PORT` | `8081` | Server port |
| `AUTH_SERVICE_URL` | `http://localhost:8080` | Auth service URL |
| `LOG_LEVEL` | `info` | Log level |

## Dependencies

This service depends on the **auth-service** for token validation. The auth-service must be running and accessible at the URL specified in `AUTH_SERVICE_URL`.

## Running the Service

### Using Docker Compose

```bash
# Build and run with database and auth-service
docker-compose up --build

# Run in background
docker-compose up -d
```

### Using Docker

```bash
# Build the image
./build_image.sh

# Run the container (make sure auth-service is running)
docker run -p 8081:8081 \
  -e DB_HOST=your-db-host \
  -e DB_USER=your-db-user \
  -e DB_PASSWORD=your-db-password \
  -e AUTH_SERVICE_URL=http://your-auth-service:8080 \
  report-processor:latest
```

### Local Development

```bash
# Install dependencies
go mod download

# Run the service (make sure auth-service is running)
go run main.go
```

## Authentication

The service uses the **auth-service** for token validation. Include the token in the Authorization header:

```
Authorization: Bearer <your-jwt-token>
```

The service will make an HTTP request to the auth-service's `/api/v3/validate-token` endpoint to validate the token and get the user ID.

## CORS

The service includes CORS middleware that allows:
- All origins (`*`)
- Common HTTP methods (GET, POST, OPTIONS, PUT, DELETE)
- Standard headers including Authorization

## Error Handling

The service returns consistent error responses:

```json
{
    "success": false,
    "message": "Error description",
    "error": "Detailed error message (optional)"
}
```

## Logging

The service uses Go's standard log package and logs:
- Service startup and shutdown
- Database operations
- HTTP request errors
- Authentication failures
- Auth service communication errors

## Dependencies

- Go 1.24+
- MySQL 8.0+
- Gin web framework
- Auth-service for token validation
- MySQL driver for Go 