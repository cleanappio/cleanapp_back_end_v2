# Areas Service Migration

This document describes the migration of area-related endpoints from the main backend to a separate microservice.

## Overview

The following endpoints have been moved to a dedicated `areas-service`:

### Health Check
- `GET /health` - Health check endpoint

### API v3 Endpoints
- `POST /api/v3/create_or_update_area` - Create or update an area
- `GET /api/v3/get_areas` - Get areas with optional filtering
- `POST /api/v3/update_consent` - Update consent for an area
- `GET /api/v3/get_areas_count` - Get count of areas

## Architecture Changes

### New Microservice Structure

```
areas-service/
├── config/
│   └── config.go          # Configuration management
├── database/
│   ├── schema.go          # Database schema initialization
│   └── service.go         # Database operations
├── handlers/
│   └── handlers.go        # HTTP handlers
├── models/
│   └── models.go          # Data structures
├── utils/
│   ├── db.go              # Database connection
│   ├── area_index.go      # Spatial indexing utilities
│   └── area_index_test.go # Spatial indexing tests
├── main.go                # Service entry point
├── Dockerfile             # Container configuration
├── docker-compose.yml     # Local development setup
└── README.md              # Service documentation
```

### Configuration Management

The service uses a centralized configuration package (`config/config.go`) that loads all settings from environment variables with sensible defaults:

- `PORT` - Service port (default: 8081)
- `DB_HOST` - Database host (default: localhost)
- `DB_PORT` - Database port (default: 3306)
- `DB_NAME` - Database name (default: cleanapp)
- `DB_USER` - Database username (default: server)
- `DB_PASSWORD` - Database password (default: secret)

### Database Schema Initialization

The service automatically creates required database tables on startup:

- `areas` - Main areas table with spatial data
- `contact_emails` - Contact email information
- `area_index` - Spatial index for efficient area queries

## Files Removed from Backend

### From `backend/server/server.go`:
- Area endpoint constants
- Area route registrations

### From `backend/server/area.go`:
- Entire file removed (all area-related handlers)

### From `backend/db/db.go`:
- `CreateOrUpdateArea()` function
- `GetAreas()` function
- `GetAreaIdsForViewport()` function
- `UpdateConsent()` function
- `GetAreasCount()` function
- `getAreaIdsForViewport()` helper function
- Area-related imports

### From `backend/server/api/api.go`:
- `Area` struct
- `ContactEmail` struct
- `CreateAreaRequest` struct
- `UpdateConsentRequest` struct
- `AreasResponse` struct
- `AreasCountResponse` struct

### From `backend/area_index/`:
- Entire directory removed (moved to areas-service/utils/)

## Migration Benefits

1. **Separation of Concerns**: Area-related functionality is now isolated
2. **Independent Deployment**: Areas service can be deployed separately
3. **Scalability**: Areas service can be scaled independently
4. **Maintainability**: Easier to maintain and test area-specific features
5. **Configuration Management**: Centralized configuration with environment variables
6. **Database Management**: Automatic schema initialization on startup

## Deployment

### Local Development

```bash
cd areas-service
go run main.go
```

### Docker Deployment

```bash
cd areas-service
docker-compose up --build
```

### Environment Variables

Set the following environment variables for production deployment:

```bash
export PORT=8081
export DB_HOST=your-mysql-host
export DB_PORT=3306
export DB_NAME=cleanapp
export DB_USER=your-db-user
export DB_PASSWORD=your-db-password
```

## Testing

The areas-service includes comprehensive tests:

```bash
cd areas-service
go test ./...
```

## API Compatibility

The API endpoints maintain the same request/response format as the original backend implementation, ensuring backward compatibility for existing clients. 