# Areas Service

A microservice for managing areas and area-related operations.

## Features

- Create or update areas
- Get areas with filtering
- Update consent for areas
- Get areas count
- Automatic database schema initialization

## Configuration

The service uses the following environment variables:

- `PORT` - Service port (default: 8081)
- `DB_HOST` - Database host (default: localhost)
- `DB_PORT` - Database port (default: 3306)
- `DB_NAME` - Database name (default: cleanapp)
- `DB_USER` - Database username (default: server)
- `DB_PASSWORD` - Database password (default: secret)

## API Endpoints

### Health Check
- `GET /health` - Health check endpoint

### API v3 Endpoints
- `POST /api/v3/create_or_update_area` - Create or update an area (🔒 **Bearer Token Required**)
- `GET /api/v3/get_areas` - Get areas with optional filtering
- `POST /api/v3/update_consent` - Update consent for an area
- `GET /api/v3/get_areas_count` - Get count of areas
- `DELETE /api/v3/delete_area` - Delete an area and all related data (🔒 **Bearer Token Required**)

## Authentication

Protected endpoints require a valid Bearer token in the Authorization header. The token is validated by calling the auth-service.

### Example Usage with Authentication

```bash
# Create or update an area (requires authentication)
curl -X POST http://localhost:8081/api/v3/create_or_update_area \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{"area": {...}}'

# Delete an area (requires authentication)
curl -X DELETE http://localhost:8081/api/v3/delete_area \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{"area_id": 123}'
```

## Running the Service

### Local Development

```bash
go run main.go
```

### Using Docker

```bash
docker-compose up --build
```

## Database Schema

The service automatically creates the following tables on startup:

- `areas` - Main areas table
- `contact_emails` - Contact email information
- `area_index` - Spatial index for areas

## Testing

```bash
go test ./...
``` 