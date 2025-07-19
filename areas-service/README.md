# Areas Service

A microservice for managing geographic areas and their associated contact information.

## Endpoints

- `POST /create_or_update_area` - Create or update an area
- `GET /get_areas` - Get areas (optionally filtered by viewport)
- `POST /update_consent` - Update email consent for an area
- `GET /get_areas_count` - Get the total count of areas

## Running the Service

### Local Development

1. Install dependencies:
```bash
go mod tidy
```

2. Run the service:
```bash
go run main.go
```

### Docker

1. Build and run with Docker Compose:
```bash
docker-compose up --build
```

2. Or build and run manually:
```bash
docker build -t areas-service .
docker run -p 8081:8081 areas-service
```

## Configuration

The service uses the following environment variables:

- `MYSQL_HOST` - MySQL host (default: localhost)
- `MYSQL_PORT` - MySQL port (default: 3306)
- `MYSQL_DB` - MySQL database name (default: cleanapp)
- `MYSQL_PASSWORD` - MySQL password (default: secret)

## API Examples

### Create or Update Area

```bash
curl -X POST http://localhost:8081/create_or_update_area \
  -H "Content-Type: application/json" \
  -d '{
    "version": "2.0",
    "area": {
      "id": 1,
      "name": "Downtown Area",
      "description": "Central business district",
      "is_custom": false,
      "contact_name": "John Doe",
      "contact_emails": [
        {
          "email": "john@example.com",
          "consent_report": true
        }
      ],
      "coordinates": {
        "type": "Feature",
        "geometry": {
          "type": "Polygon",
          "coordinates": [[[0, 0], [1, 0], [1, 1], [0, 1], [0, 0]]]
        }
      },
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  }'
```

### Get Areas

```bash
# Get all areas
curl http://localhost:8081/get_areas

# Get areas in a specific viewport
curl "http://localhost:8081/get_areas?sw_lat=0&sw_lon=0&ne_lat=1&ne_lon=1"
```

### Update Consent

```bash
curl -X POST http://localhost:8081/update_consent \
  -H "Content-Type: application/json" \
  -d '{
    "version": "2.0",
    "contact_email": {
      "email": "john@example.com",
      "consent_report": false
    }
  }'
```

### Get Areas Count

```bash
curl http://localhost:8081/get_areas_count
``` 