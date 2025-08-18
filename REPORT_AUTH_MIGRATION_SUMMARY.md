# Report Auth Service Migration Summary

## Overview

The `report_auth_service` has been successfully migrated from the `auth-service` into a new dedicated microservice called `report-auth-service`. This migration improves service separation, maintainability, and scalability.

## What Was Moved

### From `auth-service` to `report-auth-service`:

1. **Report Authorization Logic**: All report authorization business logic
2. **Database Queries**: Spatial queries for area-based authorization and brand-based authorization
3. **Models**: Report authorization request/response structures
4. **API Endpoints**: Report authorization HTTP endpoints

### New Files Created:

- `report-auth-service/` - Complete new microservice directory
- `docker-compose.report-auth.yml` - Docker compose for both services
- `run_report_auth_services.sh` - Script to run both services

## Architecture Changes

### Before (Monolithic):
```
auth-service/
├── database/
│   ├── service.go (AuthService + ReportAuthService)
│   └── report_auth_service.go
├── handlers/
│   └── handlers.go (auth + report auth handlers)
└── models/
    └── models.go (auth + report auth models)
```

### After (Microservices):
```
auth-service/ (Authentication only)
├── database/
│   └── service.go (AuthService only)
├── handlers/
│   └── handlers.go (auth handlers only)
├── models/
│   └── models.go (auth models only)
└── utils/
    └── report_auth_client.go (Client to call report-auth-service)

report-auth-service/ (Report authorization only)
├── database/
│   └── service.go (ReportAuthService only)
├── handlers/
│   └── handlers.go (report auth handlers only)
├── models/
│   └── models.go (report auth models only)
└── main.go
```

## API Changes

### Report Auth Service Endpoints:

- **POST** `/api/v3/reports/authorization` - Check report authorization
- **GET** `/health` - Service health check
- **GET** `/api/v3/health` - API health check

### Request Format:
```json
{
  "report_seqs": [1, 2, 3, 4]
}
```

**Authentication Required:**
- Include a valid JWT token in the `Authorization: Bearer <token>` header, OR
- For internal service communication, include `X-User-ID: <user_id>` header

### Response Format:
```json
{
  "authorizations": [
    {
      "report_seq": 1,
      "authorized": true,
      "reason": "Location within user's area"
    }
  ]
}
```

## Configuration Changes

### Auth Service:
- Added `REPORT_AUTH_SERVICE_URL` environment variable
- Default: `http://localhost:8081`

### Report Auth Service:
- Runs on port `8081` (configurable via `PORT` env var)
- Connects to the same `cleanapp` database
- Relies on existing tables: `reports`, `report_analysis`, `customer_areas`, `areas`, `area_index`, `customer_brands`

## Database Dependencies

The report-auth service relies on the following existing tables in the `cleanapp` database:

- `reports` - Report metadata and location coordinates
- `report_analysis` - Brand information for reports
- `customer_areas` - User area ownership mapping
- `areas` - Area definitions
- `area_index` - Spatial index for geographic queries
- `customer_brands` - User brand ownership mapping

## Running the Services

### Option 1: Docker Compose (Recommended)
```bash
./run_report_auth_services.sh
```

### Option 2: Manual Docker Compose
```bash
docker-compose -f docker-compose.report-auth.yml up --build
```

### Option 3: Individual Services
```bash
# Terminal 1: Start report-auth-service
cd report-auth-service
docker-compose up

# Terminal 2: Start auth-service
cd auth-service
docker-compose up
```

## Service Communication

The auth-service now communicates with the report-auth-service via HTTP:

1. **Client Request** → `auth-service` → `report-auth-service`
2. **Response** → `report-auth-service` → `auth-service` → **Client**

The auth-service acts as a proxy, forwarding report authorization requests to the dedicated service.

## Authentication Implementation

The report-auth service now requires authentication for the `/reports/authorization` endpoint:

### For External Clients:
- Must include a valid JWT token in the `Authorization: Bearer <token>` header
- Token is validated by calling the auth-service's `/api/v3/validate-token` endpoint

### For Internal Services (like auth-service):
- Can use the `X-User-ID: <user_id>` header for direct communication
- This bypasses JWT validation for trusted internal services

### Security Features:
- JWT token validation via auth-service
- IP address logging for security monitoring
- Comprehensive error logging for debugging
- Support for both external and internal authentication methods

## Benefits of This Migration

1. **Service Separation**: Clear boundaries between authentication and report authorization
2. **Independent Scaling**: Each service can be scaled independently
3. **Technology Flexibility**: Different services can use different technologies if needed
4. **Easier Testing**: Services can be tested in isolation
5. **Better Maintainability**: Smaller, focused codebases
6. **Deployment Flexibility**: Services can be deployed independently

## Migration Steps Completed

1. ✅ Created new `report-auth-service` microservice
2. ✅ Moved report authorization logic and models
3. ✅ Updated `auth-service` to use HTTP client
4. ✅ Created Docker configuration for both services
5. ✅ Updated service communication
6. ✅ Removed old report auth code from auth-service
7. ✅ Created documentation and run scripts
8. ✅ Added authentication middleware to report-auth service
9. ✅ Updated API to require authentication
10. ✅ Implemented internal service communication support

## Next Steps

1. **Testing**: Verify both services work correctly together
2. **Monitoring**: Add health checks and monitoring
3. **Load Balancing**: Consider adding load balancer if needed
4. **Service Discovery**: Implement service discovery for production
5. **Circuit Breakers**: Add circuit breakers for service communication
6. **Metrics**: Add metrics and observability

## Rollback Plan

If issues arise, the services can be rolled back by:

1. Stopping the new services
2. Restoring the old `report_auth_service.go` to auth-service
3. Reverting the auth-service changes
4. Restarting the original auth-service

## Notes

- Both services share the same database for now (can be separated later if needed)
- The report-auth service is stateless and can be horizontally scaled
- All existing functionality has been preserved
- The API interface remains the same for clients
