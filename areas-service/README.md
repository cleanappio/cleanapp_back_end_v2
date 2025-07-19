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

- `POST /create_or_update_area` - Create or update an area
- `GET /get_areas` - Get areas with optional filtering
- `POST /update_consent` - Update consent for an area
- `GET /get_areas_count` - Get count of areas

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