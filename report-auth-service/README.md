# Report Auth Service

A microservice for handling report authorization logic in the CleanApp system.

## Overview

This service is responsible for checking whether users are authorized to view specific reports based on:
- Geographic location (area ownership)
- Brand ownership
- Report existence and metadata

## Features

- **Location-based Authorization**: Checks if a report's location falls within user-owned areas
- **Brand-based Authorization**: Verifies brand ownership for reports with brand information
- **Spatial Queries**: Uses MySQL spatial functions for efficient geographic lookups
- **RESTful API**: Provides HTTP endpoints for authorization checks

## API Endpoints

### POST /api/v3/reports/authorization
Check authorization for multiple reports. **Requires authentication.**

**Authentication:**
- **Option 1**: Include a valid JWT token in the `Authorization: Bearer <token>` header
- **Option 2**: For internal service communication, include `X-User-ID: <user_id>` header

**Request Body:**
```json
{
  "report_seqs": [1, 2, 3, 4]
}
```

**Note**: The `user_id` is no longer required in the request body as it's extracted from the authentication token or internal service header.

**Response:**
```json
{
  "authorizations": [
    {
      "report_seq": 1,
      "authorized": true,
      "reason": "Location within user's area"
    },
    {
      "report_seq": 2,
      "authorized": false,
      "reason": "Location within another user's area"
    }
  ]
}
```

### GET /health
Service health check endpoint.

### GET /api/v3/health
API health check endpoint.

## Database Dependencies

This service relies on the following tables in the `cleanapp` database:
- `reports` - Report metadata and location
- `report_analysis` - Brand information for reports
- `customer_areas` - User area ownership
- `areas` - Area definitions
- `area_index` - Spatial index for areas
- `customer_brands` - User brand ownership

## Configuration

Environment variables:
- `DB_HOST` - Database host (default: localhost)
- `DB_PORT` - Database port (default: 3306)
- `DB_USER` - Database username (default: root)
- `DB_PASSWORD` - Database password (default: password)
- `PORT` - Service port (default: 8081)
- `TRUSTED_PROXIES` - Comma-separated list of trusted proxy IPs

## Running the Service

### Local Development
```bash
go mod tidy
go run main.go
```

### Docker
```bash
docker-compose up
```

### Build Image
```bash
./build_image.sh
```

## Architecture

The service follows a clean architecture pattern:
- **Models**: Data structures for requests/responses
- **Handlers**: HTTP request handling and validation
- **Database**: Business logic and data access
- **Middleware**: CORS, security headers, rate limiting

## Dependencies

- Go 1.23+
- Gin web framework
- MySQL driver
- MySQL 8.0+ (for spatial functions)
