# Areas Service Migration

This document describes the migration of area-related endpoints from the main backend service to a separate microservice.

## Overview

The following four endpoints have been moved to a new microservice called `areas-service`:

- `POST /create_or_update_area` - Create or update an area
- `GET /get_areas` - Get areas (optionally filtered by viewport)
- `POST /update_consent` - Update email consent for an area
- `GET /get_areas_count` - Get the total count of areas

## Changes Made

### 1. Created New Areas Service (`areas-service/`)

**Structure:**
```
areas-service/
├── main.go                 # Main application entry point
├── go.mod                  # Go module dependencies
├── Dockerfile             # Docker container definition
├── docker-compose.yml     # Docker Compose configuration
├── build_image.sh         # Build script
├── README.md              # Service documentation
├── models/
│   └── models.go          # Data structures
├── database/
│   ├── service.go         # Database operations
│   └── schema.go          # Database schema initialization
├── handlers/
│   └── handlers.go        # HTTP request handlers
└── utils/
    ├── db.go              # Database connection utilities
    ├── area_index.go      # Spatial indexing utilities
    └── area_index_test.go # Comprehensive test suite for spatial operations
```

**Key Features:**
- Runs on port 8081 (configurable)
- Uses the same database schema as the original service
- Maintains API compatibility with existing clients
- Includes Docker support for easy deployment

### 2. Modified Original Backend Service

**Removed from `backend/server/server.go`:**
- Area-related endpoint constants
- Area-related route registrations

**Removed from `backend/server/area.go`:**
- Entire file deleted (moved to areas-service)

**Removed from `backend/db/db.go`:**
- `CreateOrUpdateArea()` function
- `GetAreas()` function
- `GetAreaIdsForViewport()` function
- `UpdateConsent()` function
- `GetAreasCount()` function
- `sendAffectedPolygonsEmails()` function
- `findAreasForReport()` function
- Removed unused imports (area_index, geojson, etc.)

**Removed from `backend/area_index/`:**
- Entire `area_index` package moved to `areas-service/utils/`
- `area_index.go` - spatial indexing utilities
- `area_index_test.go` - comprehensive test suite for spatial operations

**Removed from `backend/server/api/api.go`:**
- `ContactEmail` struct
- `Area` struct
- `CreateAreaRequest` struct
- `UpdateConsentRequest` struct
- `AreasResponse` struct
- `AreasCountResponse` struct

## API Compatibility

The new areas-service maintains full API compatibility with the original endpoints:

- Same request/response formats
- Same validation rules (version 2.0 requirement)
- Same error handling patterns
- Same HTTP status codes

## Database Schema

The areas-service automatically creates its required database tables on startup:
- `areas` table - stores area information
- `contact_emails` table - stores email contacts for areas
- `area_index` table - spatial index for geographic queries

The service uses the same database schema as the original service and will create tables if they don't exist, ensuring compatibility with existing data.

## Deployment

### Local Development
```bash
cd areas-service
go mod tidy
go run main.go
```

### Docker
```bash
cd areas-service
./build_image.sh
docker-compose up
```

### Configuration
The service uses the same environment variables as the original:
- `MYSQL_HOST` - MySQL host (default: localhost)
- `MYSQL_PORT` - MySQL port (default: 3306)
- `MYSQL_DB` - MySQL database name (default: cleanapp)
- `MYSQL_PASSWORD` - MySQL password (default: secret)

## Migration Steps

1. **Deploy the new areas-service** alongside the existing backend
2. **Update client applications** to point to the new service endpoints
3. **Test thoroughly** to ensure all functionality works as expected
4. **Remove area-related code** from the original backend (already done)
5. **Monitor** both services during the transition period

## Benefits

- **Separation of Concerns**: Area management is now isolated in its own service
- **Scalability**: Areas service can be scaled independently
- **Maintainability**: Easier to maintain and update area-related functionality
- **Technology Flexibility**: Can use different technologies/versions for different services
- **Deployment Flexibility**: Can deploy areas service independently

## Testing

The areas-service includes comprehensive error handling and logging. Test the following scenarios:

1. Create a new area
2. Update an existing area
3. Get areas with and without viewport filters
4. Update email consent
5. Get areas count
6. Test with invalid data (wrong version, missing fields, etc.)
7. Test database connection failures

## Rollback Plan

If issues arise, the original area-related code can be restored by:

1. Reverting the changes to `backend/server/server.go`
2. Restoring the `backend/server/area.go` file
3. Restoring the area-related functions in `backend/db/db.go`
4. Restoring the area-related types in `backend/server/api/api.go`
5. Restoring the `backend/area_index/` package with its utilities and tests
6. Updating client applications to point back to the original endpoints 